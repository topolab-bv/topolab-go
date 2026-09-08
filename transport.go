package topolab

import (
	"bytes"
	"context"
	"encoding/json"
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
	return t.doRequest(ctx, http.MethodGet, path, query, nil)
}

// doRequest performs one request, retrying transient failures. body, when
// non-nil, is a JSON payload and is replayed on every attempt. The caller owns
// resp.Body on a nil error; on status >= 400 the body is consumed and a *Error
// is returned.
func (t *transport) doRequest(ctx context.Context, method, path string, query url.Values, body []byte) (*http.Response, error) {
	u := t.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var lastErr error
	for attempt := 0; attempt <= t.maxRetries; attempt++ {
		var payload io.Reader
		if body != nil {
			payload = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, payload)
		if err != nil {
			return nil, &Error{Kind: KindConfiguration, Message: err.Error()}
		}
		req.Header.Set("X-API-Key", t.apiKey)
		req.Header.Set("User-Agent", t.userAgent)
		req.Header.Set("Accept", "application/json")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

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
	return decodeJSON(resp, v)
}

// postJSON performs a POST with a JSON payload and decodes the JSON body into v.
func (t *transport) postJSON(ctx context.Context, path string, payload, v any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return &Error{Kind: KindValidation, Message: "encoding request: " + err.Error()}
	}
	resp, err := t.doRequest(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return err
	}
	return decodeJSON(resp, v)
}

// decodeJSON decodes resp into v and closes the body.
func decodeJSON(resp *http.Response, v any) error {
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
