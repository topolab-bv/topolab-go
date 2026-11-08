package topolab_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	topolab "github.com/topolab-bv/topolab-go"
)

// Since engine #650 a 429 carries a string code, details and, when there is a
// wait, a Retry-After header.

const rateLimitedBody = `{"code": "RATE_LIMIT_EXCEEDED", "message": "Rate limit exceeded",
  "details": {"retryAfter": 42, "limit": 300, "remaining": 0, "resetAt": "2026-11-07T12:01:00.000Z"},
  "requestId": "r429"}`

const quotaBody = `{"code": "USAGE_QUOTA_EXCEEDED", "message": "API key usage quota exceeded",
  "details": {"quota": 10, "used": 10}, "requestId": "q429"}`

func rateLimitErr(t *testing.T, body string, header map[string]string) *topolab.Error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	c, err := topolab.New(topolab.WithAPIKey(apiKey), topolab.WithBaseURL(srv.URL), topolab.WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Dataset("nl-domino-poi").Metadata(context.Background(), "")
	if !errors.Is(err, topolab.ErrRateLimit) {
		t.Fatalf("want ErrRateLimit, got %v", err)
	}
	var e *topolab.Error
	errors.As(err, &e)
	return e
}

func TestRateLimitWaitFromDetails(t *testing.T) {
	e := rateLimitErr(t, rateLimitedBody, nil)
	if e.Code != "RATE_LIMIT_EXCEEDED" || e.RetryAfter != 42 {
		t.Errorf("Code = %q, RetryAfter = %v", e.Code, e.RetryAfter)
	}
	if e.Details["limit"] != 300.0 {
		t.Errorf("Details = %#v", e.Details)
	}
}

func TestRateLimitPrefersTheRetryAfterHeader(t *testing.T) {
	e := rateLimitErr(t, rateLimitedBody, map[string]string{"Retry-After": "43"})
	if e.RetryAfter != 43 {
		t.Errorf("RetryAfter = %v, want 43", e.RetryAfter)
	}
}

func TestRateLimitReadsAnHTTPDateRetryAfter(t *testing.T) {
	when := time.Now().Add(120 * time.Second).UTC().Format(http.TimeFormat)
	e := rateLimitErr(t, `{"code": 429, "message": "x"}`, map[string]string{"Retry-After": when})
	if e.RetryAfter < 100 || e.RetryAfter > 121 {
		t.Errorf("RetryAfter = %v, want about 120", e.RetryAfter)
	}
}

func TestRateLimitIgnoresAnUnparseableRetryAfter(t *testing.T) {
	e := rateLimitErr(t, rateLimitedBody, map[string]string{"Retry-After": "soon"})
	if e.RetryAfter != 42 {
		t.Errorf("RetryAfter = %v, want 42 from details", e.RetryAfter)
	}
}

func TestUsageQuotaHasNoWaitButKeepsDetails(t *testing.T) {
	e := rateLimitErr(t, quotaBody, nil)
	if e.Code != "USAGE_QUOTA_EXCEEDED" || e.RetryAfter != 0 {
		t.Errorf("Code = %q, RetryAfter = %v", e.Code, e.RetryAfter)
	}
	if e.Details["quota"] != 10.0 || e.Details["used"] != 10.0 {
		t.Errorf("Details = %#v", e.Details)
	}
}

// The retry delay honours details.retryAfter: 50ms requested against a 500ms
// exponential fallback, so only a fast retry proves the details were read.
func TestRetryHonoursDetailsRetryAfter(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			writeJSON(w, 429, map[string]any{"code": "RATE_LIMIT_EXCEEDED", "message": "Rate limit exceeded",
				"details": map[string]any{"retryAfter": 0.05, "limit": 60, "remaining": 0}})
			return
		}
		writeJSON(w, 200, map[string]any{"id": "uuid-1", "table": "nl-domino-poi"})
	}))
	defer srv.Close()

	c, _ := topolab.New(topolab.WithAPIKey(apiKey), topolab.WithBaseURL(srv.URL), topolab.WithMaxRetries(2))
	start := time.Now()
	if _, err := c.Dataset("nl-domino-poi").Metadata(context.Background(), ""); err != nil {
		t.Fatalf("retry did not recover: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 400*time.Millisecond {
		t.Errorf("waited %v; details.retryAfter (50ms) was ignored", elapsed)
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
}

// The header wins over details: a zero-second header against a 5s details
// value must retry at once.
func TestRetryPrefersTheHeaderOverDetails(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			writeJSON(w, 429, map[string]any{"code": "RATE_LIMIT_EXCEEDED", "message": "x",
				"details": map[string]any{"retryAfter": 5}})
			return
		}
		writeJSON(w, 200, map[string]any{"id": "uuid-1", "table": "nl-domino-poi"})
	}))
	defer srv.Close()

	c, _ := topolab.New(topolab.WithAPIKey(apiKey), topolab.WithBaseURL(srv.URL), topolab.WithMaxRetries(1))
	start := time.Now()
	if _, err := c.Dataset("nl-domino-poi").Metadata(context.Background(), ""); err != nil {
		t.Fatalf("retry did not recover: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 2*time.Second {
		t.Errorf("waited %v; the Retry-After header (0s) should win over details.retryAfter (5s)", elapsed)
	}
}
