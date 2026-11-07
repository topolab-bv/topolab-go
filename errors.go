package topolab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// Kind classifies a Topolab API error. Match it with errors.Is against the
// exported sentinel values, or read it from a *[Error] obtained via errors.As.
type Kind string

const (
	KindConfiguration      Kind = "configuration"
	KindAuthentication     Kind = "authentication"
	KindAddonRequired      Kind = "addon_required"
	KindAccessDenied       Kind = "access_denied"
	KindInsufficientCredit Kind = "insufficient_credits"
	KindNotFound           Kind = "not_found"
	KindQueryTimeout       Kind = "query_timeout"
	KindRateLimit          Kind = "rate_limit"
	KindValidation         Kind = "validation"
	KindServer             Kind = "server"
	KindConnection         Kind = "connection"
)

// Code is the error envelope's code. It is a named string code such as
// "SPENDING_LIMIT_EXCEEDED" when the engine throws one, and otherwise the
// numeric HTTP status written in decimal ("403"), much as [json.Number] holds a
// number in a string.
//
// Compare named codes directly (err.Code == "SPENDING_LIMIT_EXCEEDED"). For the
// HTTP status read [Error.StatusCode], which is always numeric; [Code.Int]
// reports the numeric form when the envelope carried one.
type Code string

// Int returns the code as an int and true when it is numeric, such as 402. It
// returns 0 and false for a named code and for the empty code.
func (c Code) Int() (int, bool) {
	if c == "" {
		return 0, false
	}
	n, err := strconv.Atoi(string(c))
	if err != nil {
		return 0, false
	}
	return n, true
}

// IsNumeric reports whether the code is the numeric HTTP status rather than a
// named code.
func (c Code) IsNumeric() bool {
	_, ok := c.Int()
	return ok
}

// UnmarshalJSON accepts a JSON string or an integer. Any other value leaves the
// code empty instead of failing, so one odd field never hides the rest of an
// error body; the caller then falls back to the HTTP status.
func (c *Code) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*c = Code(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		if i, err := n.Int64(); err == nil {
			*c = Code(strconv.FormatInt(i, 10))
			return nil
		}
	}
	*c = ""
	return nil
}

// Error is the single error type returned for every Topolab failure. The Kind
// field classifies it; category-specific fields (Addon, RetryAfter, Required,
// Available) are populated where relevant.
//
// Kind is chosen from the HTTP status, never from the body. Code refines it: a
// 403 whose Code is "SPENDING_LIMIT_EXCEEDED" is still [KindAccessDenied].
//
// Use errors.Is with a sentinel (e.g. [ErrRateLimit]) to branch on category, and
// errors.As(&Error) to read the typed fields.
type Error struct {
	Kind       Kind
	StatusCode int    // HTTP status, always numeric; 0 for connection errors
	Message    string // server-supplied message
	RequestID  string // x-request-id, if present

	// Code is the envelope's code: a named code such as
	// "SPENDING_LIMIT_EXCEEDED", or the numeric HTTP status ("403") when the
	// error has none or the body carries no usable code. Empty for errors
	// raised before a response exists.
	Code Code
	// Details holds the envelope's allow-listed details (for example limit,
	// consumed, remaining and periodEnd with SPENDING_LIMIT_EXCEEDED), or nil
	// when the body has none. JSON numbers decode as float64.
	Details map[string]any

	Addon      string  // KindAddonRequired: required add-on slug (api-access, archived-data, …)
	RetryAfter float64 // KindRateLimit: seconds to wait (0 if unknown)
	Required   int     // KindInsufficientCredit
	Available  int     // KindInsufficientCredit
}

func (e *Error) Error() string {
	if e.StatusCode > 0 {
		if e.Code != "" && !e.Code.IsNumeric() {
			return fmt.Sprintf("topolab: %s (%d %s): %s", e.Kind, e.StatusCode, e.Code, e.Message)
		}
		return fmt.Sprintf("topolab: %s (%d): %s", e.Kind, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("topolab: %s: %s", e.Kind, e.Message)
}

// Is reports whether target is a sentinel of the same Kind, enabling
// errors.Is(err, topolab.ErrNotFound) and friends.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Kind == e.Kind && t.StatusCode == 0 && t.Message == ""
}

// Sentinels for errors.Is. They carry only a Kind.
var (
	ErrConfiguration      = &Error{Kind: KindConfiguration}
	ErrAuthentication     = &Error{Kind: KindAuthentication}
	ErrAddonRequired      = &Error{Kind: KindAddonRequired}
	ErrAccessDenied       = &Error{Kind: KindAccessDenied}
	ErrInsufficientCredit = &Error{Kind: KindInsufficientCredit}
	ErrNotFound           = &Error{Kind: KindNotFound}
	ErrQueryTimeout       = &Error{Kind: KindQueryTimeout}
	ErrRateLimit          = &Error{Kind: KindRateLimit}
	ErrValidation         = &Error{Kind: KindValidation}
	ErrServer             = &Error{Kind: KindServer}
	ErrConnection         = &Error{Kind: KindConnection}
)

// addonRe extracts the add-on from a 403 message. Add-on identifiers are
// hyphenated slugs, and the requirement is phrased two ways — "requires the
// api-access add-on" and "requires the Archived Data add-on." — so the capture
// is lazy and unrestricted, then normalised by normaliseAddon.
var addonRe = regexp.MustCompile(`(?i)requires the (.+?) add-?on`)

// normaliseAddon turns a captured add-on name into its canonical slug:
// "api-access" and "Archived Data" both yield the hyphenated lower-case form.
func normaliseAddon(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), "-")
}

// errorBody is the shape of a Topolab JSON error response:
// {code, message, path, method, time, requestId}, plus details for some errors.
// There is no statusCode field, and code is a named string code or the numeric
// status, so the Kind always comes from the HTTP status; code only refines it.
type errorBody struct {
	Code       Code           `json:"code"`
	Message    string         `json:"message"`
	Error      string         `json:"error"`
	RequestID  string         `json:"requestId"`
	RetryAfter *float64       `json:"retryAfter"`
	Details    map[string]any `json:"details"`
}

// detailInt reads a numeric detail as an int, or 0 when it is absent or not a
// number.
func detailInt(details map[string]any, key string) int {
	if v, ok := details[key].(float64); ok {
		return int(v)
	}
	return 0
}

// errorFromResponse maps an HTTP error response (status >= 400) to a *Error.
func errorFromResponse(status int, body errorBody, header http.Header) *Error {
	msg := body.Message
	if msg == "" {
		msg = body.Error
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	// The request id is the X-Request-Id header, falling back to the body.
	requestID := header.Get("x-request-id")
	if requestID == "" {
		requestID = body.RequestID
	}
	code := body.Code
	if code == "" {
		code = Code(strconv.Itoa(status))
	}
	e := &Error{StatusCode: status, Message: msg, RequestID: requestID, Code: code, Details: body.Details}

	switch {
	case status == http.StatusUnauthorized:
		e.Kind = KindAuthentication
	case status == http.StatusPaymentRequired:
		e.Kind = KindInsufficientCredit
		e.Required = detailInt(body.Details, "required")
		e.Available = detailInt(body.Details, "available")
	case status == http.StatusForbidden:
		if m := addonRe.FindStringSubmatch(msg); m != nil {
			e.Kind = KindAddonRequired
			e.Addon = normaliseAddon(m[1])
		} else {
			e.Kind = KindAccessDenied
		}
	case status == http.StatusNotFound:
		e.Kind = KindNotFound
	case status == http.StatusRequestTimeout:
		e.Kind = KindQueryTimeout
	case status == http.StatusTooManyRequests:
		e.Kind = KindRateLimit
		e.RetryAfter = retryAfterSeconds(body, header)
	case status == http.StatusBadRequest:
		if reOrg.MatchString(msg) {
			e.Kind = KindConfiguration
		} else {
			e.Kind = KindValidation
		}
	case status >= 500:
		e.Kind = KindServer
	default:
		e.Kind = KindValidation
	}
	return e
}

var reOrg = regexp.MustCompile(`(?i)organization`)

func retryAfterSeconds(body errorBody, header http.Header) float64 {
	if body.RetryAfter != nil {
		return *body.RetryAfter
	}
	if h := header.Get("retry-after"); h != "" {
		if v, err := strconv.ParseFloat(h, 64); err == nil {
			return v
		}
	}
	return 0
}
