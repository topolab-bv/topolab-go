package topolab_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	topolab "github.com/topolab-bv/topolab-go"
)

// Since engine #641 the envelope's code is a string when the error has its own
// code (upper-snake, such as SPENDING_LIMIT_EXCEEDED) and otherwise the numeric
// HTTP status. Bodies are inlined so these tests do not depend on fixtures that
// land in topolab-sdk-spec separately.

const spendingLimitBody = `{
  "path": "/v1/dataset/nl-domino-poi/files/geojson", "method": "GET",
  "code": "SPENDING_LIMIT_EXCEEDED",
  "message": "Organization monthly spending limit exceeded",
  "details": {"limit": 5000, "consumed": 4995, "remaining": 5,
              "periodEnd": "2026-11-30T23:59:59.999Z"},
  "time": "2026-11-07T11:52:28.000Z", "requestId": "8f0c41d2"
}`

const creditsBody = `{
  "path": "/v1/dataset/nl-domino-poi/files/geojson", "method": "GET",
  "code": 402, "message": "Insufficient credits",
  "details": {"required": 10, "available": 0},
  "time": "2026-11-07T11:52:28.000Z", "requestId": "6a1b3c5d"
}`

func serveRaw(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func asAPIError(t *testing.T, err error) *topolab.Error {
	t.Helper()
	var apiErr *topolab.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *topolab.Error, got %T: %v", err, err)
	}
	return apiErr
}

func TestStringCodeIsKeptAndStatusComesFromTheResponse(t *testing.T) {
	srv := serveRaw(t, 403, spendingLimitBody)
	_, err := newClient(t, srv).Dataset("nl-domino-poi").ToGeoJSON(context.Background())

	// The Kind still follows the HTTP status; the code refines it.
	if !errors.Is(err, topolab.ErrAccessDenied) {
		t.Fatalf("want ErrAccessDenied, got %v", err)
	}
	e := asAPIError(t, err)
	if e.Code != "SPENDING_LIMIT_EXCEEDED" {
		t.Errorf("Code = %q", e.Code)
	}
	if e.Code.IsNumeric() {
		t.Errorf("a named code reported as numeric")
	}
	if n, ok := e.Code.Int(); ok || n != 0 {
		t.Errorf("Int() = %d, %v; want 0, false", n, ok)
	}
	if e.StatusCode != 403 {
		t.Errorf("StatusCode = %d", e.StatusCode)
	}
	want := map[string]any{"limit": 5000.0, "consumed": 4995.0, "remaining": 5.0, "periodEnd": "2026-11-30T23:59:59.999Z"}
	if !reflect.DeepEqual(e.Details, want) {
		t.Errorf("Details = %#v", e.Details)
	}
	if e.RequestID != "8f0c41d2" {
		t.Errorf("RequestID = %q", e.RequestID)
	}
	if got := e.Error(); got != "topolab: access_denied (403 SPENDING_LIMIT_EXCEEDED): Organization monthly spending limit exceeded" {
		t.Errorf("Error() = %q", got)
	}
}

func TestNumericCodeIsTheStatusAndDetailsAreExposed(t *testing.T) {
	srv := serveRaw(t, 402, creditsBody)
	_, err := newClient(t, srv).Dataset("nl-domino-poi").ToGeoJSON(context.Background())

	if !errors.Is(err, topolab.ErrInsufficientCredit) {
		t.Fatalf("want ErrInsufficientCredit, got %v", err)
	}
	e := asAPIError(t, err)
	if e.Code != "402" || !e.Code.IsNumeric() {
		t.Errorf("Code = %q, numeric %v", e.Code, e.Code.IsNumeric())
	}
	if n, ok := e.Code.Int(); !ok || n != 402 {
		t.Errorf("Int() = %d, %v; want 402, true", n, ok)
	}
	if e.StatusCode != 402 {
		t.Errorf("StatusCode = %d", e.StatusCode)
	}
	if !reflect.DeepEqual(e.Details, map[string]any{"required": 10.0, "available": 0.0}) {
		t.Errorf("Details = %#v", e.Details)
	}
	if e.Required != 10 || e.Available != 0 {
		t.Errorf("Required/Available = %d/%d", e.Required, e.Available)
	}
	// The numeric case keeps the 0.1 message format.
	if got := e.Error(); got != "topolab: insufficient_credits (402): Insufficient credits" {
		t.Errorf("Error() = %q", got)
	}
}

func TestStringCodeWithoutDetailsFromSQL(t *testing.T) {
	srv := serveRaw(t, 400, `{"path": "/v1/sql/query", "method": "POST", "code": "SQL_DATA_ERROR",
	  "message": "The query raised a data error (SQLSTATE 22P02) while evaluating a value.",
	  "time": "2026-11-07T11:52:28.000Z", "requestId": "3c5e7a9b"}`)
	_, err := newClient(t, srv).SQL(context.Background(), "SELECT 'x'::int", nil)

	if !errors.Is(err, topolab.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
	e := asAPIError(t, err)
	if e.Code != "SQL_DATA_ERROR" || e.StatusCode != 400 || e.Details != nil {
		t.Errorf("Code = %q, StatusCode = %d, Details = %#v", e.Code, e.StatusCode, e.Details)
	}
}

func TestCodeFallsBackToTheStatus(t *testing.T) {
	bodies := []string{
		`{"message": "no code at all"}`,
		`{"code": null, "message": "x"}`,
		`{"code": "", "message": "x"}`,
		`{"code": true, "message": "x"}`,
		`{"code": {"nested": 1}, "message": "x"}`,
		`{"code": 4.5, "message": "x"}`,
		`<html>Not Found</html>`,
	}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			srv := serveRaw(t, 404, body)
			_, err := newClient(t, srv).Dataset("nope").Metadata(context.Background(), "")
			if !errors.Is(err, topolab.ErrNotFound) {
				t.Fatalf("want ErrNotFound, got %v", err)
			}
			e := asAPIError(t, err)
			if e.Code != "404" || e.Details != nil {
				t.Errorf("Code = %q, Details = %#v", e.Code, e.Details)
			}
		})
	}
}

// An odd code must not stop the rest of the body from decoding.
func TestOddCodeKeepsTheRestOfTheBody(t *testing.T) {
	srv := serveRaw(t, 404, `{"code": {"nested": 1}, "message": "kept", "requestId": "rid", "details": ["not", "an", "object"]}`)
	_, err := newClient(t, srv).Dataset("nope").Metadata(context.Background(), "")
	e := asAPIError(t, err)
	if e.Message != "kept" || e.RequestID != "rid" || e.Details != nil {
		t.Errorf("Message = %q, RequestID = %q, Details = %#v", e.Message, e.RequestID, e.Details)
	}
}

func TestCodeUnmarshalJSON(t *testing.T) {
	cases := map[string]topolab.Code{
		`"SPENDING_LIMIT_EXCEEDED"`: "SPENDING_LIMIT_EXCEEDED",
		`403`:                       "403",
		` 402 `:                     "402",
		`null`:                      "",
		`true`:                      "",
		`[1]`:                       "",
		`4.5`:                       "",
	}
	for in, want := range cases {
		var c topolab.Code
		if err := json.Unmarshal([]byte(in), &c); err != nil {
			t.Errorf("%s: unexpected error %v", in, err)
		}
		if c != want {
			t.Errorf("%s: got %q, want %q", in, c, want)
		}
	}
}

func TestConnectionErrorsHaveNoCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	c, err := topolab.New(topolab.WithAPIKey(apiKey), topolab.WithBaseURL(srv.URL), topolab.WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	srv.Close()
	_, err = c.Dataset("x").Metadata(context.Background(), "")
	if !errors.Is(err, topolab.ErrConnection) {
		t.Fatalf("want ErrConnection, got %v", err)
	}
	e := asAPIError(t, err)
	if e.Code != "" || e.Details != nil || e.StatusCode != 0 {
		t.Errorf("Code = %q, Details = %#v, StatusCode = %d", e.Code, e.Details, e.StatusCode)
	}
}
