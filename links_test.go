package topolab

// Download and Archive through the link route (POST /v1/dataset/:table/exports).
//
// The link route answers 200 with a 10-minute signed URL, or 202 while the file
// is being prepared. The SDK fetches the signed URL with a new http.Client and
// a request that carries no API key. A server without the route (404, or 405)
// gets today's call to the old route instead, decided on every call. 402, 403
// and 5xx are returned as errors.
//
// Two httptest servers play the API and the storage host, so the signed fetch
// crosses hosts as it does in production. The 404 body is what staging answered
// on 2026-11-24 for POST /v1/dataset/dining_drinking_mcdonalds/exports.
//
// This file is in package topolab so it can replace the poll clock and the
// retry backoff on the client's transport.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	lkTable    = "dining_drinking_mcdonalds"
	lkBuild    = "7d3f6c2a-1b4e-4c8d-9a0f-5e6b7c8d9e0f"
	lkKey      = "tlb_test_key"
	lkLink     = "POST /v1/dataset/" + lkTable + "/exports"
	lkFilesCSV = "GET /v1/dataset/" + lkTable + "/files/csv"
	lkArchive  = "GET /v1/dataset/" + lkTable + "/archives/2026-11/csv"
	lkSignedP  = "/platform-staging-datasets/exports/" + lkTable + "/2026-12/standard/20261202T020000Z-a1b2c3d4-w1/" + lkTable + "_2026-12.csv"
	lkSigned   = "GET " + lkSignedP
	lkQuery    = "X-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Credential=engine%40example.iam.gserviceaccount.com%2F20261203%2Fauto%2Fstorage%2Fgoog4_request&X-Goog-Date=20261203T100000Z&X-Goog-Expires=600&X-Goog-SignedHeaders=host&X-Goog-Signature=5f2d0c9e"
	lkLinkCSV  = "id,location_name,city\n1,McDonald's Amsterdam Centraal,Amsterdam\n"
	lkOldCSV   = "id,location_name,city\n1,McDonald's Utrecht Centraal,Utrecht\n"
	lkZip      = "PK\x03\x04dining_drinking_mcdonalds_2026-11.csv"
)

type lkReply struct {
	status  int
	json    any
	body    string
	headers map[string]string
}

type lkSeen struct {
	route  string
	uri    string
	header http.Header
	body   string
}

// lkServer answers "METHOD /path" routes. A list answers in turn and repeats
// its last reply; an unknown route is a 599, so a stray call fails loudly
// instead of looking like a fallback.
type lkServer struct {
	*httptest.Server
	mu     sync.Mutex
	routes map[string][]lkReply
	served map[string]int
	seen   []lkSeen
}

func newLkServer(t *testing.T, routes map[string][]lkReply) *lkServer {
	t.Helper()
	s := &lkServer{routes: routes, served: map[string]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		route := r.Method + " " + r.URL.Path
		s.mu.Lock()
		s.seen = append(s.seen, lkSeen{route: route, uri: r.RequestURI, header: r.Header.Clone(), body: string(body)})
		list := s.routes[route]
		n := s.served[route]
		s.served[route] = n + 1
		s.mu.Unlock()
		if len(list) == 0 {
			w.WriteHeader(599)
			_, _ = w.Write([]byte("unmocked " + route))
			return
		}
		reply := list[min(n, len(list)-1)]
		for k, v := range reply.headers {
			w.Header().Set(k, v)
		}
		payload := []byte(reply.body)
		if reply.json != nil {
			w.Header().Set("Content-Type", "application/json")
			payload, _ = json.Marshal(reply.json)
		}
		w.WriteHeader(reply.status)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *lkServer) count(route string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, x := range s.seen {
		if x.route == route {
			n++
		}
	}
	return n
}

func (s *lkServer) first(route string) lkSeen {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.seen {
		if x.route == route {
			return x
		}
	}
	return lkSeen{}
}

func (s *lkServer) all(route string) []lkSeen {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []lkSeen
	for _, x := range s.seen {
		if x.route == route {
			out = append(out, x)
		}
	}
	return out
}

func lkReady(storage *lkServer, month string) lkReply {
	return lkReply{status: 200, json: map[string]any{
		"state": "ready", "url": storage.URL + lkSignedP + "?" + lkQuery,
		"expiresAt": "2026-12-03T10:10:00.000Z", "filename": lkTable + "_" + month + ".csv",
		"bytes": len(lkLinkCSV), "sha256": strings.Repeat("9b2f6a0c", 8),
		"tier": "standard", "month": month, "build": lkBuild,
	}}
}

func lkPreparing(seconds any, headers map[string]string) lkReply {
	body := map[string]any{"state": "preparing", "build": lkBuild, "statusUrl": "/v1/dataset/exports/" + lkBuild}
	if seconds != nil {
		body["retryAfterSeconds"] = seconds
	}
	return lkReply{status: 202, json: body, headers: headers}
}

var lkNoRoute = lkReply{status: 404, json: map[string]any{
	"path": "/v1/dataset/" + lkTable + "/exports", "method": "POST", "code": 404,
	"message": "Cannot POST /v1/dataset/" + lkTable + "/exports",
	"time":    "2026-11-23T23:43:35.228Z", "requestId": "2b12c5708156347fdeffee2d7be873a7",
}}

var lkBuildFailed = lkReply{status: 503, headers: map[string]string{"Retry-After": "0"}, json: map[string]any{
	"code": "FILE_BUILD_FAILED", "message": "This file could not be built.",
	"details": map[string]any{"retryAfter": 0, "linkRoute": "/v1/dataset/" + lkTable + "/exports"},
}}

func one(r lkReply) []lkReply { return []lkReply{r} }

// lkClock records the poll waits instead of sleeping; the clock moves with them.
type lkClock struct {
	now   time.Time
	waits []time.Duration
}

// lkDataset builds a client against api with no retry backoff and a fake poll
// clock, and returns its dataset handle.
func lkDataset(t *testing.T, api *lkServer) (*Dataset, *lkClock) {
	t.Helper()
	c, err := New(WithAPIKey(lkKey), WithBaseURL(api.URL))
	if err != nil {
		t.Fatal(err)
	}
	clock := &lkClock{now: time.Date(2026, 12, 3, 10, 0, 0, 0, time.UTC)}
	c.t.backoffBase = 0
	c.t.now = func() time.Time { return clock.now }
	c.t.pollWait = func(_ context.Context, d time.Duration) bool {
		clock.waits = append(clock.waits, d)
		clock.now = clock.now.Add(d)
		return true
	}
	return c.Dataset(lkTable), clock
}

func lkRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func seconds(ws []time.Duration) []float64 {
	out := []float64{}
	for _, w := range ws {
		out = append(out, w.Seconds())
	}
	return out
}

// --- ready ----------------------------------------------------------------

func TestLinkDownloadFetchesTheSignedURLWithoutTheKey(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{lkSigned: one(lkReply{status: 200, body: lkLinkCSV})})
	api := newLkServer(t, map[string][]lkReply{
		lkLink:     one(lkReady(storage, "2026-12")),
		lkFilesCSV: one(lkReply{status: 200, body: lkOldCSV}),
	})
	ds, clock := lkDataset(t, api)
	out := filepath.Join(t.TempDir(), "mcdonalds.csv")

	if err := ds.Download(context.Background(), out, "csv"); err != nil {
		t.Fatal(err)
	}

	if got := lkRead(t, out); got != lkLinkCSV {
		t.Errorf("file = %q", got)
	}
	posted := api.first(lkLink)
	if posted.header.Get("X-API-Key") != lkKey {
		t.Errorf("link route key = %q", posted.header.Get("X-API-Key"))
	}
	if posted.header.Get("Content-Type") != "application/json" {
		t.Errorf("link route content type = %q", posted.header.Get("Content-Type"))
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(posted.body), &body)
	if want := map[string]any{"format": "csv", "bundle": "file"}; !reflect.DeepEqual(body, want) {
		t.Errorf("link body = %v, want %v", body, want)
	}
	fetched := storage.first(lkSigned)
	if v, ok := fetched.header["X-Api-Key"]; ok {
		t.Errorf("signed URL request carried the API key: %v", v)
	}
	if fetched.header.Get("Authorization") != "" {
		t.Errorf("signed URL request carried Authorization")
	}
	if fetched.header.Get("Accept") == "application/json" {
		t.Errorf("signed URL request carried the API's Accept header")
	}
	if want := lkSignedP + "?" + lkQuery; fetched.uri != want {
		t.Errorf("signed request URI = %q, want it byte for byte %q", fetched.uri, want)
	}
	if api.count(lkFilesCSV) != 0 {
		t.Errorf("old route called")
	}
	if len(clock.waits) != 0 {
		t.Errorf("waited %v", clock.waits)
	}
}

func TestLinkArchiveSendsTheMonthAndAsksForTheZip(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{lkSigned: one(lkReply{status: 200, body: lkZip})})
	api := newLkServer(t, map[string][]lkReply{
		lkLink:    one(lkReady(storage, "2026-11")),
		lkArchive: one(lkReply{status: 200, body: lkZip}),
	})
	ds, _ := lkDataset(t, api)
	dir := t.TempDir()

	if err := ds.Archive(context.Background(), filepath.Join(dir, "november.zip"), "2026-11", "csv"); err != nil {
		t.Fatal(err)
	}
	if err := ds.Archive(context.Background(), filepath.Join(dir, "latest.zip"), "LATEST", ""); err != nil {
		t.Fatal(err)
	}

	var bodies []map[string]any
	for _, s := range api.all(lkLink) {
		var b map[string]any
		_ = json.Unmarshal([]byte(s.body), &b)
		bodies = append(bodies, b)
	}
	want := []map[string]any{
		{"format": "csv", "month": "2026-11", "bundle": "zip"},
		{"format": "geojson", "month": "latest", "bundle": "zip"},
	}
	if !reflect.DeepEqual(bodies, want) {
		t.Errorf("link bodies = %v, want %v", bodies, want)
	}
	if got := lkRead(t, filepath.Join(dir, "november.zip")); got != lkZip {
		t.Errorf("file = %q", got)
	}
	if api.count(lkArchive) != 0 {
		t.Errorf("old route called")
	}
}

// --- preparing ------------------------------------------------------------------

func TestLinkPollsHonouringRetryAfterSeconds(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{lkSigned: one(lkReply{status: 200, body: lkLinkCSV})})
	api := newLkServer(t, map[string][]lkReply{
		lkLink: {lkPreparing(4, nil), lkPreparing(2, nil), lkReady(storage, "2026-12")},
	})
	ds, clock := lkDataset(t, api)
	out := filepath.Join(t.TempDir(), "x.csv")

	if err := ds.Download(context.Background(), out, "csv"); err != nil {
		t.Fatal(err)
	}

	if got := seconds(clock.waits); !reflect.DeepEqual(got, []float64{4, 2}) {
		t.Errorf("waits = %v", got)
	}
	if api.count(lkLink) != 3 || storage.count(lkSigned) != 1 {
		t.Errorf("link %d, signed %d", api.count(lkLink), storage.count(lkSigned))
	}
	if got := lkRead(t, out); got != lkLinkCSV {
		t.Errorf("file = %q", got)
	}
}

func TestLinkPollWaitReadsTheHeaderAndNeverSpins(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{lkSigned: one(lkReply{status: 200, body: lkLinkCSV})})
	api := newLkServer(t, map[string][]lkReply{
		lkLink: {
			lkPreparing(nil, map[string]string{"Retry-After": "7"}),
			lkPreparing(nil, nil),
			lkPreparing(0, nil),
			lkReady(storage, "2026-12"),
		},
	})
	ds, clock := lkDataset(t, api)

	if err := ds.Download(context.Background(), filepath.Join(t.TempDir(), "x.csv"), "csv"); err != nil {
		t.Fatal(err)
	}

	// header 7 s; nothing at all: 5 s; a zero: the 1 s floor.
	if got := seconds(clock.waits); !reflect.DeepEqual(got, []float64{7, 5, 1}) {
		t.Errorf("waits = %v", got)
	}
}

func TestLinkGivesUpAfterTheWaitBudgetWithFilePreparing(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{lkSigned: one(lkReply{status: 200, body: lkLinkCSV})})
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkPreparing(700, nil))})
	ds, clock := lkDataset(t, api)
	out := filepath.Join(t.TempDir(), "x.csv")

	err := ds.Download(context.Background(), out, "csv")

	var apiErr *Error
	if !errors.As(err, &apiErr) || !errors.Is(err, ErrServer) {
		t.Fatalf("want a server *Error, got %v", err)
	}
	if apiErr.Code != "FILE_PREPARING" || apiErr.StatusCode != 202 {
		t.Errorf("code %q, status %d", apiErr.Code, apiErr.StatusCode)
	}
	if want := map[string]any{"build": lkBuild, "retryAfter": float64(700)}; !reflect.DeepEqual(apiErr.Details, want) {
		t.Errorf("details = %v, want %v", apiErr.Details, want)
	}
	// A third wait would end at 2,100 s, past the 1,800 s limit.
	if got := seconds(clock.waits); !reflect.DeepEqual(got, []float64{700, 700}) {
		t.Errorf("waits = %v", got)
	}
	if api.count(lkLink) != 3 || storage.count(lkSigned) != 0 {
		t.Errorf("link %d, signed %d", api.count(lkLink), storage.count(lkSigned))
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("file written")
	}
}

func TestLinkStopsWaitingWhenTheContextEnds(t *testing.T) {
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkPreparing(4, nil))})
	ds, _ := lkDataset(t, api)
	ctx, cancel := context.WithCancel(context.Background())
	ds.t.pollWait = func(ctx context.Context, d time.Duration) bool {
		cancel()
		return ds.t.sleep(ctx, d)
	}

	err := ds.Download(ctx, filepath.Join(t.TempDir(), "x.csv"), "csv")

	if !errors.Is(err, ErrConnection) {
		t.Fatalf("want ErrConnection, got %v", err)
	}
}

// --- failed and unexpected answers --------------------------------------------------

func TestLinkFailedBuildIsReturnedAndNeverFallsBack(t *testing.T) {
	api := newLkServer(t, map[string][]lkReply{
		lkLink:     one(lkBuildFailed),
		lkFilesCSV: one(lkReply{status: 200, body: lkOldCSV}),
	})
	ds, _ := lkDataset(t, api)
	out := filepath.Join(t.TempDir(), "x.csv")

	err := ds.Download(context.Background(), out, "csv")

	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != KindServer {
		t.Fatalf("want a server *Error, got %v", err)
	}
	if apiErr.Code != "FILE_BUILD_FAILED" || apiErr.StatusCode != 503 {
		t.Errorf("code %q, status %d", apiErr.Code, apiErr.StatusCode)
	}
	if n := api.count(lkLink); n != 4 {
		t.Errorf("link calls = %d, want 4 (1 try + 3 retries, as today)", n)
	}
	if api.count(lkFilesCSV) != 0 {
		t.Errorf("old route called")
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("file written")
	}
}

func TestLinkServerErrorNeverFallsBack(t *testing.T) {
	api := newLkServer(t, map[string][]lkReply{
		lkLink:     one(lkReply{status: 500, json: map[string]any{"code": 500, "message": "Internal server error"}}),
		lkFilesCSV: one(lkReply{status: 200, body: lkOldCSV}),
	})
	ds, _ := lkDataset(t, api)

	err := ds.Download(context.Background(), filepath.Join(t.TempDir(), "x.csv"), "csv")

	if !errors.Is(err, ErrServer) {
		t.Fatalf("want ErrServer, got %v", err)
	}
	if api.count(lkLink) != 4 || api.count(lkFilesCSV) != 0 {
		t.Errorf("link %d, old %d", api.count(lkLink), api.count(lkFilesCSV))
	}
}

func TestLinkAnswerWithoutALinkIsAServerError(t *testing.T) {
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReply{status: 200, json: map[string]any{"state": "ready"}})})
	ds, _ := lkDataset(t, api)

	err := ds.Download(context.Background(), filepath.Join(t.TempDir(), "x.csv"), "csv")

	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != KindServer || apiErr.StatusCode != 200 {
		t.Fatalf("want a server *Error with status 200, got %v", err)
	}
}

// --- fallback on 404 and 405 ----------------------------------------------------------

func TestLink404FallsBackToTheFilesRoute(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{lkSigned: one(lkReply{status: 200, body: lkLinkCSV})})
	api := newLkServer(t, map[string][]lkReply{
		lkLink:     one(lkNoRoute),
		lkFilesCSV: one(lkReply{status: 200, body: lkOldCSV}),
	})
	ds, _ := lkDataset(t, api)
	out := filepath.Join(t.TempDir(), "x.csv")

	if err := ds.Download(context.Background(), out, "csv"); err != nil {
		t.Fatal(err)
	}

	if got := lkRead(t, out); got != lkOldCSV {
		t.Errorf("file = %q", got)
	}
	if api.count(lkLink) != 1 || api.count(lkFilesCSV) != 1 || storage.count(lkSigned) != 0 {
		t.Errorf("link %d, old %d, signed %d", api.count(lkLink), api.count(lkFilesCSV), storage.count(lkSigned))
	}
	if got := api.first(lkFilesCSV).header.Get("X-API-Key"); got != lkKey {
		t.Errorf("old route key = %q", got)
	}
}

func TestLink405FallsBackToTheArchiveRoute(t *testing.T) {
	api := newLkServer(t, map[string][]lkReply{
		lkLink:    one(lkReply{status: 405, json: map[string]any{"code": 405, "message": "Method Not Allowed"}}),
		lkArchive: one(lkReply{status: 200, body: lkZip}),
	})
	ds, _ := lkDataset(t, api)
	out := filepath.Join(t.TempDir(), "x.zip")

	if err := ds.Archive(context.Background(), out, "2026-11", "csv"); err != nil {
		t.Fatal(err)
	}

	if got := lkRead(t, out); got != lkZip {
		t.Errorf("file = %q", got)
	}
	if api.count(lkArchive) != 1 {
		t.Errorf("archive route calls = %d", api.count(lkArchive))
	}
}

func TestLinkFallbackKeepsTheOldRoutesRetriesAndAnswer(t *testing.T) {
	api := newLkServer(t, map[string][]lkReply{
		lkLink: one(lkNoRoute),
		lkFilesCSV: {
			{status: 503, headers: map[string]string{"Retry-After": "0"}, json: map[string]any{
				"code": "FILE_PREPARING", "message": "This file is being prepared.", "details": map[string]any{"retryAfter": 0},
			}},
			{status: 200, body: lkOldCSV},
		},
	})
	ds, _ := lkDataset(t, api)
	out := filepath.Join(t.TempDir(), "x.csv")

	if err := ds.Download(context.Background(), out, "csv"); err != nil {
		t.Fatal(err)
	}

	if api.count(lkFilesCSV) != 2 {
		t.Errorf("old route calls = %d, want 2", api.count(lkFilesCSV))
	}
	if got := lkRead(t, out); got != lkOldCSV {
		t.Errorf("file = %q", got)
	}
}

func TestLinkFallbackReturnsTheOldRoutes404(t *testing.T) {
	api := newLkServer(t, map[string][]lkReply{
		lkLink:    one(lkNoRoute),
		lkArchive: one(lkReply{status: 404, json: map[string]any{"code": 404, "message": "No archive for 2026-11"}}),
	})
	ds, _ := lkDataset(t, api)

	err := ds.Archive(context.Background(), filepath.Join(t.TempDir(), "x.zip"), "2026-11", "csv")

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestLinkFallbackIsDecidedOnEveryCall(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{lkSigned: one(lkReply{status: 200, body: lkLinkCSV})})
	api := newLkServer(t, map[string][]lkReply{
		lkLink:     {lkNoRoute, lkReady(storage, "2026-12")},
		lkFilesCSV: one(lkReply{status: 200, body: lkOldCSV}),
	})
	ds, _ := lkDataset(t, api)
	dir := t.TempDir()

	if err := ds.Download(context.Background(), filepath.Join(dir, "first.csv"), "csv"); err != nil {
		t.Fatal(err)
	}
	if err := ds.Download(context.Background(), filepath.Join(dir, "second.csv"), "csv"); err != nil {
		t.Fatal(err)
	}

	if lkRead(t, filepath.Join(dir, "first.csv")) != lkOldCSV || lkRead(t, filepath.Join(dir, "second.csv")) != lkLinkCSV {
		t.Errorf("files = %q, %q", lkRead(t, filepath.Join(dir, "first.csv")), lkRead(t, filepath.Join(dir, "second.csv")))
	}
	if got := []int{api.count(lkLink), api.count(lkFilesCSV), storage.count(lkSigned)}; !reflect.DeepEqual(got, []int{2, 1, 1}) {
		t.Errorf("link, old, signed = %v", got)
	}
}

// --- 402 and 403 are returned ---------------------------------------------------------

func TestLink403IsReturnedWithoutAFallback(t *testing.T) {
	api := newLkServer(t, map[string][]lkReply{
		lkLink:     one(lkReply{status: 403, json: map[string]any{"code": 403, "message": "Your organization has no licence for this dataset"}}),
		lkFilesCSV: one(lkReply{status: 200, body: lkOldCSV}),
	})
	ds, _ := lkDataset(t, api)
	out := filepath.Join(t.TempDir(), "x.csv")

	err := ds.Download(context.Background(), out, "csv")

	if !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("want ErrAccessDenied, got %v", err)
	}
	if api.count(lkFilesCSV) != 0 {
		t.Errorf("old route called")
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("file written")
	}
}

func TestLink402IsReturnedWithoutAFallback(t *testing.T) {
	api := newLkServer(t, map[string][]lkReply{
		lkLink: one(lkReply{status: 402, json: map[string]any{
			"code": 402, "message": "Insufficient credits", "details": map[string]any{"required": 20, "available": 3},
		}}),
		lkArchive: one(lkReply{status: 200, body: lkZip}),
	})
	ds, _ := lkDataset(t, api)

	err := ds.Archive(context.Background(), filepath.Join(t.TempDir(), "x.zip"), "2026-11", "csv")

	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != KindInsufficientCredit || apiErr.Required != 20 {
		t.Fatalf("want KindInsufficientCredit with Required 20, got %v", err)
	}
	if api.count(lkArchive) != 0 {
		t.Errorf("old route called")
	}
}

// --- the signed URL ----------------------------------------------------------------------

func TestLinkSignedURLErrorOmitsTheBodyAndLeavesNoFile(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{lkSigned: one(lkReply{
		status: 400, headers: map[string]string{"Content-Type": "application/xml"},
		body: "<?xml version='1.0' encoding='UTF-8'?><Error><Code>ExpiredToken</Code><Details>X-Goog-Signature=5f2d0c9e</Details></Error>",
	})})
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReady(storage, "2026-12"))})
	ds, _ := lkDataset(t, api)
	dir := t.TempDir()

	err := ds.Download(context.Background(), filepath.Join(dir, "x.csv"), "csv")

	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != 400 {
		t.Fatalf("want a 400 *Error, got %v", err)
	}
	if strings.Contains(err.Error(), "X-Goog-Signature") {
		t.Errorf("error repeats the storage body: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("left files behind: %v", entries)
	}
}

func TestLinkSignedURLRetriesATransientAnswer(t *testing.T) {
	storage := newLkServer(t, map[string][]lkReply{lkSigned: {
		{status: 503, headers: map[string]string{"Retry-After": "0"}, body: "<Error/>"},
		{status: 200, body: lkLinkCSV},
	}})
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReady(storage, "2026-12"))})
	ds, _ := lkDataset(t, api)
	out := filepath.Join(t.TempDir(), "x.csv")

	if err := ds.Download(context.Background(), out, "csv"); err != nil {
		t.Fatal(err)
	}

	if storage.count(lkSigned) != 2 {
		t.Errorf("signed calls = %d, want 2", storage.count(lkSigned))
	}
	if got := lkRead(t, out); got != lkLinkCSV {
		t.Errorf("file = %q", got)
	}
}

// --- Sample is unchanged --------------------------------------------------------------------

func TestSampleReadsTheSampleRouteAndNeverTheLinkRoute(t *testing.T) {
	sample := "GET /v1/dataset/" + lkTable + "/sample/geojson"
	api := newLkServer(t, map[string][]lkReply{
		sample: one(lkReply{status: 200, json: map[string]any{"type": "FeatureCollection", "features": []any{}}}),
	})
	ds, _ := lkDataset(t, api)

	b, err := ds.Sample(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(b), "FeatureCollection") {
		t.Errorf("sample = %s", b)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.seen) != 1 || api.seen[0].route != sample {
		t.Errorf("requests = %v", api.seen)
	}
}

// --- the signed query never reaches an error ---------------------------------------------

// lkNoQuery fails when err's text carries any part of the signed link's query.
func lkNoQuery(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("want an error, got nil")
	}
	for _, text := range []string{err.Error(), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		for _, leak := range []string{"X-Goog-Signature", "5f2d0c9e", "X-Goog-Credential", "?X-Goog"} {
			if strings.Contains(text, leak) {
				t.Fatalf("error carries the signed query (%q): %s", leak, text)
			}
		}
	}
}

func TestLinkSignedURLConnectionErrorOmitsTheQuery(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReply{status: 200, json: map[string]any{
		"state": "ready", "url": closedURL + lkSignedP + "?" + lkQuery, "build": lkBuild,
	}})})
	ds, _ := lkDataset(t, api)

	err := ds.Download(context.Background(), filepath.Join(t.TempDir(), "x.csv"), "csv")

	lkNoQuery(t, err)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != KindConnection {
		t.Fatalf("want a connection *Error, got %v", err)
	}
	// Still says where it failed: the host and the path, without the query.
	if !strings.Contains(err.Error(), strings.TrimPrefix(closedURL, "http://")) || !strings.Contains(err.Error(), lkSignedP) {
		t.Errorf("error does not name the host and path: %v", err)
	}
}

func TestLinkSignedURLTimeoutOmitsTheQuery(t *testing.T) {
	release := make(chan struct{})
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(release); storage.Close() })
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReply{status: 200, json: map[string]any{
		"state": "ready", "url": storage.URL + lkSignedP + "?" + lkQuery, "build": lkBuild,
	}})})
	c, err := New(WithAPIKey(lkKey), WithBaseURL(api.URL), WithTimeout(100*time.Millisecond), WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}

	err = c.Dataset(lkTable).Download(context.Background(), filepath.Join(t.TempDir(), "x.csv"), "csv")

	lkNoQuery(t, err)
}

func TestLinkSignedURLWithAnUnsupportedSchemeOmitsTheQuery(t *testing.T) {
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReply{status: 200, json: map[string]any{
		"state": "ready", "url": "ftp://storage.example" + lkSignedP + "?" + lkQuery, "build": lkBuild,
	}})})
	ds, _ := lkDataset(t, api)

	err := ds.Download(context.Background(), filepath.Join(t.TempDir(), "x.csv"), "csv")

	lkNoQuery(t, err)
}

// lkNoQueryAtAll fails when err's text carries any query parameter of either
// signed link, or a "?" (no part of a query belongs in an error).
func lkNoQueryAtAll(t *testing.T, err error, queries ...string) {
	t.Helper()
	lkNoQuery(t, err)
	for _, text := range []string{err.Error(), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		if strings.Contains(text, "?") {
			t.Fatalf("error carries a query: %s", text)
		}
		for _, q := range queries {
			for _, pair := range strings.Split(q, "&") {
				if strings.Contains(text, pair) {
					t.Fatalf("error carries %q of a signed query: %s", pair, text)
				}
			}
		}
	}
}

func TestArchiveSignedURLConnectionErrorOmitsTheQuery(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReply{status: 200, json: map[string]any{
		"state": "ready", "url": closedURL + lkSignedP + "?" + lkQuery, "build": lkBuild,
	}})})
	ds, _ := lkDataset(t, api)

	err := ds.Archive(context.Background(), filepath.Join(t.TempDir(), "x.zip"), "2026-11", "csv")

	lkNoQueryAtAll(t, err, lkQuery)
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Kind != KindConnection {
		t.Fatalf("want a connection *Error, got %v", err)
	}
}

func TestLinkSignedURLRedirectToAnUnreachableHostOmitsBothQueries(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	const second = "X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=R2KEY%2F20261203&X-Amz-Signature=c0ffee77"
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, closedURL+"/mirror/x.csv?"+second, http.StatusFound)
	}))
	t.Cleanup(storage.Close)
	api := newLkServer(t, map[string][]lkReply{lkLink: one(lkReply{status: 200, json: map[string]any{
		"state": "ready", "url": storage.URL + lkSignedP + "?" + lkQuery, "build": lkBuild,
	}})})
	ds, _ := lkDataset(t, api)

	err := ds.Download(context.Background(), filepath.Join(t.TempDir(), "x.csv"), "csv")

	lkNoQueryAtAll(t, err, lkQuery, second)
	if strings.Contains(err.Error(), "c0ffee77") || strings.Contains(err.Error(), "R2KEY") {
		t.Fatalf("error carries the redirect's signature: %v", err)
	}
}
