package topolab

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
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

	Addon      string  // KindAddonRequired: required add-on (API_ACCESS / GIS_ACCESS)
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
	ErrRateLimit          = &Error{Kind: KindRateLimit}
	ErrValidation         = &Error{Kind: KindValidation}
	ErrServer             = &Error{Kind: KindServer}
	ErrConnection         = &Error{Kind: KindConnection}
)

var addonRe = regexp.MustCompile(`(?i)requires the (\w+) add-?on`)

// errorBody is the shape of a Topolab JSON error response.
type errorBody struct {
	Message    string   `json:"message"`
	Error      string   `json:"error"`
	StatusCode int      `json:"statusCode"`
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
	e := &Error{StatusCode: status, Message: msg, RequestID: header.Get("x-request-id")}

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
			e.Addon = m[1]
		} else {
			e.Kind = KindAccessDenied
		}
	case status == http.StatusNotFound:
		e.Kind = KindNotFound
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
