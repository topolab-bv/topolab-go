package topolab

import (
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

// Error is the single error type returned for every Topolab failure. The Kind
// field classifies it; category-specific fields (Addon, RetryAfter, Required,
// Available) are populated where relevant.
//
// Use errors.Is with a sentinel (e.g. [ErrRateLimit]) to branch on category, and
// errors.As(&Error) to read the typed fields.
type Error struct {
	Kind       Kind
	StatusCode int    // HTTP status, 0 for connection errors
	Message    string // server-supplied message
	RequestID  string // x-request-id, if present

	Addon      string  // KindAddonRequired: required add-on slug (api-access, archived-data, …)
	RetryAfter float64 // KindRateLimit: seconds to wait (0 if unknown)
	Required   int     // KindInsufficientCredit
	Available  int     // KindInsufficientCredit
}

func (e *Error) Error() string {
	if e.StatusCode > 0 {
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
// {code, message, path, method, time, requestId}. There is no statusCode field,
// and the body's code merely mirrors the HTTP status, so it is deliberately not
// decoded: mapping always branches on the HTTP status, never on the body.
type errorBody struct {
	Message    string   `json:"message"`
	Error      string   `json:"error"`
	RequestID  string   `json:"requestId"`
	RetryAfter *float64 `json:"retryAfter"`
	Details    struct {
		Required  int `json:"required"`
		Available int `json:"available"`
	} `json:"details"`
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
	e := &Error{StatusCode: status, Message: msg, RequestID: requestID}

	switch {
	case status == http.StatusUnauthorized:
		e.Kind = KindAuthentication
	case status == http.StatusPaymentRequired:
		e.Kind = KindInsufficientCredit
		e.Required = body.Details.Required
		e.Available = body.Details.Available
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
