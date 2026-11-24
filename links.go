package topolab

// Downloads through the link route, with a per-call fallback to the old routes.
//
// POST /v1/dataset/{table}/exports answers 200 with a short-lived signed URL to
// a ready file, or 202 while the file is being prepared. The signed URL is
// fetched with a new http.Client and a request that carries no API key.
//
// A server without the link route answers it with 404 (or 405). The call then
// goes to the old route (GET .../files/{format} or
// GET .../archives/{month}/{format}) with today's behaviour and retries.
// Nothing is remembered between calls, so a program keeps working while
// servers gain or lose the route. Every other error (402, 403, 5xx, ...) is
// returned.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// linkFallbackStatus holds the link route answers that mean "this server has no
// link route".
var linkFallbackStatus = map[int]bool{http.StatusNotFound: true, http.StatusMethodNotAllowed: true}

const (
	// defaultPollWait is the wait between polls when the server names none.
	defaultPollWait = 5 * time.Second
	// minPollWait is the shortest wait between polls, whatever the server says.
	minPollWait = time.Second
	// maxLinkWait is how long one call waits for a file that is being
	// prepared. The build keeps going on the server, so a later call finds the
	// file ready.
	maxLinkWait = 30 * time.Minute
)

// linkRequest is the JSON body of POST /v1/dataset/{table}/exports.
type linkRequest struct {
	Format string `json:"format"`
	Month  string `json:"month,omitempty"`
	Bundle string `json:"bundle"`
}

// linkAnswer is a 200 or 202 from the link route.
type linkAnswer struct {
	State             string   `json:"state"`
	URL               string   `json:"url"`
	Build             any      `json:"build"`
	RetryAfterSeconds *float64 `json:"retryAfterSeconds"`

	status int
	wait   time.Duration
}

// fetchViaLink writes the file the link route names to path, or the old
// route's file on a server without the link route.
func (d *Dataset) fetchViaLink(ctx context.Context, body linkRequest, fallbackPath, path string) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return &Error{Kind: KindValidation, Message: "encoding request: " + err.Error()}
	}
	started := d.t.clock()
	for {
		resp, err := d.t.doRequest(ctx, http.MethodPost, "/v1/dataset/"+d.Slug+"/exports", nil, payload)
		if err != nil {
			var apiErr *Error
			if errors.As(err, &apiErr) && linkFallbackStatus[apiErr.StatusCode] {
				return d.streamToFile(ctx, fallbackPath, path)
			}
			return err
		}
		answer, err := readLinkAnswer(resp)
		if err != nil {
			return err
		}
		if answer.status == http.StatusOK && answer.State == "ready" {
			return d.fetchSignedToFile(ctx, answer.URL, path)
		}
		if d.t.clock().Sub(started)+answer.wait > maxLinkWait {
			return stillPreparing(answer)
		}
		if !d.t.waitPoll(ctx, answer.wait) {
			return &Error{Kind: KindConnection, Message: ctx.Err().Error()}
		}
	}
}

// readLinkAnswer decodes a 2xx from the link route and closes the body. It
// returns a ready answer (with a URL) or a preparing one (with its wait), and a
// server error for anything else.
func readLinkAnswer(resp *http.Response) (*linkAnswer, error) {
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var a linkAnswer
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, unexpectedLinkAnswer(resp.StatusCode)
	}
	a.status = resp.StatusCode
	if a.status == http.StatusAccepted || a.State == "preparing" {
		secs := -1.0
		if a.RetryAfterSeconds != nil && validSeconds(*a.RetryAfterSeconds) {
			secs = *a.RetryAfterSeconds
		} else if h, ok := headerSeconds(resp.Header.Get("Retry-After")); ok {
			secs = h
		}
		a.wait = defaultPollWait
		if secs >= 0 {
			a.wait = time.Duration(secs * float64(time.Second))
		}
		if a.wait < minPollWait {
			a.wait = minPollWait
		}
		a.State = "preparing"
		return &a, nil
	}
	if a.status == http.StatusOK && a.State == "ready" && a.URL != "" {
		return &a, nil
	}
	return nil, unexpectedLinkAnswer(resp.StatusCode)
}

func unexpectedLinkAnswer(status int) *Error {
	return &Error{
		Kind:       KindServer,
		StatusCode: status,
		Code:       Code(strconv.Itoa(status)),
		Message:    "the link route answered HTTP " + strconv.Itoa(status) + " without a ready link or a preparing state",
	}
}

func stillPreparing(a *linkAnswer) *Error {
	minutes := strconv.Itoa(int(math.Round(maxLinkWait.Minutes())))
	return &Error{
		Kind:       KindServer,
		StatusCode: a.status,
		Code:       "FILE_PREPARING",
		Message: "the file is still being prepared, and the next wait would pass this call's " + minutes +
			"-minute limit; it keeps building on the server, so call again later",
		Details: map[string]any{"build": a.Build, "retryAfter": a.wait.Seconds()},
	}
}

// fetchSignedToFile streams a signed link to path. The request goes through a
// new http.Client (sharing only the configured RoundTripper, so proxy and TLS
// settings still apply) and carries the User-Agent and no API key: the link
// carries its own authorisation, so the key never reaches the storage host.
func (d *Dataset) fetchSignedToFile(ctx context.Context, rawURL, path string) error {
	if err := ensureDir(path); err != nil {
		return err
	}
	u, err := d.t.resolve(rawURL)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: d.t.httpClient.Transport, Timeout: d.t.httpClient.Timeout}
	resp, err := d.t.send(ctx, client, http.MethodGet, u, nil, signedLinkFailure(u), func(req *http.Request) {
		req.Header.Set("User-Agent", d.t.userAgent)
	}, signedLinkError)
	if err != nil {
		return err
	}
	if err := writeAtomically(resp, path); err != nil {
		var apiErr *Error
		if errors.As(err, &apiErr) {
			apiErr.Message = withoutSignedQuery(apiErr.Message, u)
		}
		return err
	}
	return nil
}

// signedLinkFailure describes an error raised before the storage host answered
// (a refused connection, a timeout, an unreadable link) without the link's
// query. The query is the link's signature: a working bearer link to the file
// for ten minutes, so it never goes into an error, which programs log. Go's
// *url.Error quotes the whole URL, so its message is rebuilt from the
// operation, the link without its query, and the cause.
func signedLinkFailure(rawURL string) func(error) string {
	return func(err error) string {
		msg := err.Error()
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			msg = urlErr.Op + " " + strconv.Quote(linkWithoutQuery(urlErr.URL)) + ": " + urlErr.Err.Error()
		}
		return withoutSignedQuery(msg, rawURL)
	}
}

// linkWithoutQuery is a link without its query and fragment: the scheme, host
// and path, which say where a fetch failed and authorise nothing.
func linkWithoutQuery(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}

// withoutSignedQuery removes every copy of rawURL's query from msg, whatever
// text it came in.
func withoutSignedQuery(msg, rawURL string) string {
	queries := []string{}
	if _, q, ok := strings.Cut(rawURL, "?"); ok && q != "" {
		queries = append(queries, q)
		if parsed, err := url.Parse(rawURL); err == nil && parsed.RawQuery != "" && parsed.RawQuery != q {
			queries = append(queries, parsed.RawQuery)
		}
	}
	for _, q := range queries {
		msg = strings.ReplaceAll(msg, "?"+q, "")
		msg = strings.ReplaceAll(msg, q, "[query removed]")
	}
	return msg
}

// resolve turns a link into an absolute URL: an absolute link as it is, a
// relative one against the base URL.
func (t *transport) resolve(raw string) (string, error) {
	ref, err := url.Parse(raw)
	if err != nil {
		return "", &Error{Kind: KindServer, Message: "the link route answered an unreadable link"}
	}
	if ref.IsAbs() {
		return raw, nil
	}
	base, err := url.Parse(t.baseURL + "/")
	if err != nil {
		return "", &Error{Kind: KindConfiguration, Message: err.Error()}
	}
	return base.ResolveReference(ref).String(), nil
}

// signedLinkError is the error for a failed signed-link fetch. The kind
// follows the status as everywhere else; the message never repeats the
// storage host's body, which can quote the link's signature.
func signedLinkError(resp *http.Response) *Error {
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	e := errorFromResponse(resp.StatusCode, errorBody{}, resp.Header)
	e.Message = "fetching the file from its signed link failed with HTTP " + strconv.Itoa(resp.StatusCode) +
		"; call again for a new link"
	return e
}

// clock reports the time the poll budget is measured against.
func (t *transport) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// waitPoll waits d between polls, or until ctx is done; false if ctx ended.
func (t *transport) waitPoll(ctx context.Context, d time.Duration) bool {
	if t.pollWait != nil {
		return t.pollWait(ctx, d)
	}
	return t.sleep(ctx, d)
}
