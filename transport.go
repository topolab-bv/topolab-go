package topolab

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"time"
)

var retryStatus = map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true}

type transport struct {
	apiKey      string
	baseURL     string
	httpClient  *http.Client
	maxRetries  int
	userAgent   string
	backoffBase time.Duration
}

// do performs a GET against path with query params, retrying transient failures.
// The caller owns resp.Body on a nil error. On status >= 400 the body is
// consumed and a *Error is returned.
func (t *transport) do(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	u := t.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var lastErr error
	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, &Error{Kind: KindConfiguration, Message: err.Error()}
		}
		req.Header.Set("X-API-Key", t.apiKey)
		req.Header.Set("User-Agent", t.userAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := t.httpClient.Do(req)
		if err != nil {
			// Honour caller cancellation immediately — never retry it.
			if ctx.Err() != nil {
				return nil, &Error{Kind: KindConnection, Message: ctx.Err().Error()}
			}
			lastErr = &Error{Kind: KindConnection, Message: err.Error()}
			if attempt >= t.maxRetries {
				return nil, lastErr
			}
			if !t.sleep(ctx, t.backoff(attempt, 0)) {
				return nil, &Error{Kind: KindConnection, Message: ctx.Err().Error()}
			}
			continue
		}

		if retryStatus[resp.StatusCode] && attempt < t.maxRetries {
			ra := parseRetryAfter(resp.Header)
			resp.Body.Close()
			if !t.sleep(ctx, t.backoff(attempt, ra)) {
				return nil, &Error{Kind: KindConnection, Message: ctx.Err().Error()}
			}
			continue
		}
		if resp.StatusCode >= 400 {
			return nil, t.errorFrom(resp)
		}
		return resp, nil
	}
	return nil, lastErr
}

// getJSON performs a GET and decodes the JSON body into v.
func (t *transport) getJSON(ctx context.Context, path string, query url.Values, v any) error {
	resp, err := t.do(ctx, path, query)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return &Error{Kind: KindServer, Message: "decoding response: " + err.Error()}
	}
	return nil
}

func (t *transport) errorFrom(resp *http.Response) *Error {
	defer resp.Body.Close()
	var body errorBody
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(data, &body)
	return errorFromResponse(resp.StatusCode, body, resp.Header)
}

func (t *transport) backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return retryAfter
	}
	return time.Duration(float64(t.backoffBase) * math.Pow(2, float64(attempt)))
}

// sleep waits d or until ctx is done; returns false if ctx was cancelled.
func (t *transport) sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func parseRetryAfter(h http.Header) time.Duration {
	if v := h.Get("retry-after"); v != "" {
		if secs, err := time.ParseDuration(v + "s"); err == nil {
			return secs
		}
	}
	return 0
}

// asError unwraps any error to a *Error, wrapping unknowns as KindConnection.
func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return &Error{Kind: KindConnection, Message: err.Error()}
}
