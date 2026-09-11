package topolab_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	topolab "github.com/topolab-bv/topolab-go"
)

// Golden fixtures are shared across the SDKs and live in the sibling spec repo.
const ownedFixtureDir = "../topolab-sdk-spec/fixtures/owned"

func ownedFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(ownedFixtureDir, name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return data
}

func serveFixture(t *testing.T, status int, name string) *httptest.Server {
	t.Helper()
	body := ownedFixture(t, name)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
}

// --- owned ---

func TestOwnedPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/dataset/owned" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Errorf("limit = %q, want 2", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(ownedFixture(t, "owned.json"))
	}))
	defer srv.Close()

	page, err := newClient(t, srv).Datasets.Owned(context.Background(), &topolab.OwnedOptions{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	// total counts every licensed dataset, not the page.
	if page.Total != 193 || page.Limit != 2 || len(page.Items) != 2 {
		t.Fatalf("unexpected page: total=%d limit=%d items=%d", page.Total, page.Limit, len(page.Items))
	}
	first := page.Items[0]
	if first.Table != "business_professional_services_autocrew" {
		t.Errorf("table = %q", first.Table)
	}
	if first.RecordCount == nil || *first.RecordCount != 119 {
		t.Errorf("recordCount = %v", first.RecordCount)
	}
	if first.LatestArchiveMonth == nil || *first.LatestArchiveMonth != "2026-07" {
		t.Errorf("latestArchiveMonth = %v", first.LatestArchiveMonth)
	}
	if first.ArchiveMonthsAvailable != 11 {
		t.Errorf("archiveMonthsAvailable = %d", first.ArchiveMonthsAvailable)
	}
	if !strings.HasPrefix(first.Links.Current, "https://") || !strings.Contains(first.Links.Current, "{format}") {
		t.Errorf("links.current = %q, want an absolute URL with a {format} placeholder", first.Links.Current)
	}
	if first.Links.LatestArchive == nil || !strings.Contains(*first.Links.LatestArchive, "{format}") {
		t.Errorf("links.latestArchive = %v", first.Links.LatestArchive)
	}
}

func TestOwnedNullableFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"items": []map[string]any{{
				"table": "t", "name": "T",
				"recordCount": nil, "latestArchiveMonth": nil,
				"archiveMonthsAvailable": 0,
				"links": map[string]any{
					"current":       "https://api.topolab.nl/v1/dataset/t/files/{format}",
					"archives":      "https://api.topolab.nl/v1/dataset/t/archives/list",
					"latestArchive": nil,
				},
			}},
			"total": 1, "limit": 50, "offset": 0,
		})
	}))
	defer srv.Close()

	page, err := newClient(t, srv).Datasets.Owned(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	it := page.Items[0]
	if it.RecordCount != nil || it.LatestArchiveMonth != nil || it.Links.LatestArchive != nil {
		t.Fatalf("nulls not preserved: %+v", it)
	}
}

func TestOwnedOptionsValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call server")
	}))
	defer srv.Close()
	c := newClient(t, srv)
	for _, opts := range []*topolab.OwnedOptions{{Limit: 201}, {Limit: -1}, {Offset: -1}} {
		if _, err := c.Datasets.Owned(context.Background(), opts); !errors.Is(err, topolab.ErrValidation) {
			t.Errorf("%+v: want ErrValidation, got %v", opts, err)
		}
	}
}

// ownedPagingServer serves `total` synthetic datasets, honouring limit/offset.
func ownedPagingServer(t *testing.T, total int, requests *int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(requests, 1)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit == 0 {
			limit = 50
		}
		offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		items := []map[string]any{}
		for i := offset; i < offset+limit && i < total; i++ {
			items = append(items, map[string]any{
				"table": "t" + strconv.Itoa(i), "name": "T" + strconv.Itoa(i),
				"links": map[string]any{"current": "https://api.topolab.nl/v1/dataset/t/files/{format}"},
			})
		}
		writeJSON(w, 200, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
	}))
}

func TestIterOwnedPagesWithoutGapsOrRepeats(t *testing.T) {
	var requests int32
	srv := ownedPagingServer(t, 5, &requests)
	defer srv.Close()

	seen := map[string]int{}
	var order []string
	for d, err := range newClient(t, srv).Datasets.IterOwned(context.Background(), &topolab.IterOwnedOptions{PageSize: 2}) {
		if err != nil {
			t.Fatal(err)
		}
		seen[d.Table]++
		order = append(order, d.Table)
	}
	if len(order) != 5 {
		t.Fatalf("iterated %d datasets, want 5 (%v)", len(order), order)
	}
	for i := 0; i < 5; i++ {
		if seen["t"+strconv.Itoa(i)] != 1 {
			t.Errorf("t%d seen %d times, want exactly 1", i, seen["t"+strconv.Itoa(i)])
		}
	}
	// 2 + 2 + 1: the short final page ends iteration.
	if requests != 3 {
		t.Errorf("made %d requests, want 3", requests)
	}
}

func TestIterOwnedTotalLimit(t *testing.T) {
	var requests int32
	srv := ownedPagingServer(t, 100, &requests)
	defer srv.Close()

	n := 0
	for _, err := range newClient(t, srv).Datasets.IterOwned(context.Background(), &topolab.IterOwnedOptions{PageSize: 2, TotalLimit: 3}) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 3 {
		t.Fatalf("iterated %d, want 3", n)
	}
}

func TestIterOwnedTerminatesOnShortPage(t *testing.T) {
	// total over-reports; a short page must still end iteration.
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if requests > 4 {
			t.Fatal("iteration did not terminate")
		}
		writeJSON(w, 200, map[string]any{
			"items": []map[string]any{{"table": "only", "name": "Only", "links": map[string]any{}}},
			"total": 999, "limit": 50, "offset": 0,
		})
	}))
	defer srv.Close()

	n := 0
	for _, err := range newClient(t, srv).Datasets.IterOwned(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != 1 || requests != 1 {
		t.Fatalf("iterated %d over %d requests, want 1/1", n, requests)
	}
}

func TestIterOwnedYieldsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 403, map[string]any{"code": 403, "message": "This endpoint requires the api-access add-on"})
	}))
	defer srv.Close()

	var got error
	for d, err := range newClient(t, srv).Datasets.IterOwned(context.Background(), nil) {
		if d != nil {
			t.Fatal("dataset yielded alongside an error")
		}
		got = err
	}
	if !errors.Is(got, topolab.ErrAddonRequired) {
		t.Fatalf("want ErrAddonRequired, got %v", got)
	}
}

// --- archives ---

func TestArchivesNewestFirst(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/dataset/ds/archives/list" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(ownedFixture(t, "archives.json")) // bare array, no envelope
	}))
	defer srv.Close()

	archives, err := newClient(t, srv).Dataset("ds").Archives(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 11 {
		t.Fatalf("got %d archives, want 11", len(archives))
	}
	if archives[0].Month != "2026-07" || archives[len(archives)-1].Month != "2025-09" {
		t.Errorf("not newest-first: %q … %q", archives[0].Month, archives[len(archives)-1].Month)
	}
	for i := 1; i < len(archives); i++ {
		if archives[i-1].Month <= archives[i].Month {
			t.Fatalf("month %q not newer than %q", archives[i-1].Month, archives[i].Month)
		}
	}
	if len(archives[0].Formats) != 5 || archives[0].ArchiveDate != "2026-07-01" {
		t.Errorf("unexpected first entry: %+v", archives[0])
	}
}

func TestArchiveMonthValidation(t *testing.T) {
	valid := []struct{ in, want string }{
		{"latest", "latest"},
		{"LATEST", "latest"},
		{"", "latest"},
		{"2026-07", "2026-07"},
		{"2026-07-15", "2026-07-15"},
		{"2024-02-29", "2024-02-29"}, // leap year
	}
	invalid := []string{"2026-13", "2026-00", "2026-07-99", "2026-02-29", "2026-7", "julyish", "2026", "2026-07-15T00:00:00Z"}

	for _, tc := range valid {
		var gotPath string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			_, _ = w.Write([]byte("zip-bytes"))
		}))
		out := filepath.Join(t.TempDir(), "a.zip")
		if err := newClient(t, srv).Dataset("ds").Archive(context.Background(), out, tc.in, "csv"); err != nil {
			t.Errorf("%q: %v", tc.in, err)
		}
		if want := "/v1/dataset/ds/archives/" + tc.want + "/csv"; gotPath != want {
			t.Errorf("%q: path = %q, want %q", tc.in, gotPath, want)
		}
		srv.Close()
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("server called for an invalid month: %s", r.URL.Path)
	}))
	defer srv.Close()
	c := newClient(t, srv)
	for _, m := range invalid {
		err := c.Dataset("ds").Archive(context.Background(), filepath.Join(t.TempDir(), "a.zip"), m, "csv")
		if !errors.Is(err, topolab.ErrValidation) {
			t.Errorf("%q: want ErrValidation, got %v", m, err)
		}
	}
	if err := c.Dataset("ds").Archive(context.Background(), "a.zip", "latest", "xlsx"); !errors.Is(err, topolab.ErrValidation) {
		t.Errorf("bad format: want ErrValidation, got %v", err)
	}
}

func TestArchiveStreamsToDisk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("PK\x03\x04archive"))
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "nested", "ds-2026-07.zip")
	if err := newClient(t, srv).Dataset("ds").Archive(context.Background(), out, "2026-07", "geojson"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if !strings.HasPrefix(string(data), "PK") {
		t.Errorf("unexpected contents %q", data)
	}
}

func TestArchiveServerErrors(t *testing.T) {
	// A well-formed month with no archive available (also out-of-window and
	// not-yet-started months) is a 404.
	srv := serveFixture(t, 404, "error-404-archive.json")
	defer srv.Close()
	err := newClient(t, srv).Dataset("ds").Archive(context.Background(), filepath.Join(t.TempDir(), "a.zip"), "2026-07", "csv")
	if !errors.Is(err, topolab.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	// A month the client cannot reject locally but the server can: 400.
	bad := serveFixture(t, 400, "error-400-month.json")
	defer bad.Close()
	err = newClient(t, bad).Dataset("ds").Archive(context.Background(), filepath.Join(t.TempDir(), "a.zip"), "2026-07", "csv")
	if !errors.Is(err, topolab.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
	var apiErr *topolab.Error
	if !errors.As(err, &apiErr) || apiErr.RequestID != "20dd1c40c296014ac5d6cd8ac153d5e8" {
		t.Errorf("requestId not taken from the body: %+v", apiErr)
	}
}

// --- coordinates ---

func TestCoordinatesReadsHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/dataset/ds/coordinates" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Errorf("limit = %q", got)
		}
		if got := r.URL.Query().Get("offset"); got != "10" {
			t.Errorf("offset = %q", got)
		}
		w.Header().Set("X-Total-Count", "119")
		w.Header().Set("X-Returned-Count", "2")
		w.Header().Set("X-Offset", "10")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(ownedFixture(t, "coordinates.json")) // bare array
	}))
	defer srv.Close()

	page, err := newClient(t, srv).Dataset("ds").Coordinates(context.Background(), &topolab.CoordinatesOptions{Limit: 2, Offset: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 119 || page.Returned != 2 || page.Offset != 10 {
		t.Fatalf("paging = total %d, returned %d, offset %d", page.Total, page.Returned, page.Offset)
	}
	if len(page.Rows) != 2 {
		t.Fatalf("got %d rows", len(page.Rows))
	}
	row := page.Rows[0]
	// latitude/longitude are JSON strings and must not be coerced.
	if row.Latitude != "51.49638600" || row.Longitude != "3.65532100" {
		t.Errorf("lat/lon = %q/%q, want the verbatim strings", row.Latitude, row.Longitude)
	}
	lon, lat, ok := row.Location.Point()
	if !ok || lon != 3.655321 || lat != 51.496386 {
		t.Errorf("location = %v,%v,%v", lon, lat, ok)
	}
	if row.Metadata["city"] != "Middelburg" {
		t.Errorf("metadata city = %v", row.Metadata["city"])
	}
}

func TestCoordinatesToleratesMissingHeaders(t *testing.T) {
	cases := []struct {
		name    string
		headers map[string]string
	}{
		{"absent", nil},
		{"unparseable", map[string]string{"X-Total-Count": "many", "X-Returned-Count": "", "X-Offset": "n/a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(ownedFixture(t, "coordinates.json"))
			}))
			defer srv.Close()

			page, err := newClient(t, srv).Dataset("ds").Coordinates(context.Background(), nil)
			if err != nil {
				t.Fatalf("missing headers must not error: %v", err)
			}
			if page.Total != 2 || page.Returned != 2 || page.Offset != 0 {
				t.Fatalf("fallbacks wrong: total %d, returned %d, offset %d", page.Total, page.Returned, page.Offset)
			}
		})
	}
}

func TestCoordinatesOptionsValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("should not call server")
	}))
	defer srv.Close()
	ds := newClient(t, srv).Dataset("ds")
	for _, opts := range []*topolab.CoordinatesOptions{{Limit: 50001}, {Limit: -1}, {Offset: -1}} {
		if _, err := ds.Coordinates(context.Background(), opts); !errors.Is(err, topolab.ErrValidation) {
			t.Errorf("%+v: want ErrValidation, got %v", opts, err)
		}
	}
}

// --- sql ---

func TestSQL(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/sql/query" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(ownedFixture(t, "sql-result.json"))
	}))
	defer srv.Close()

	res, err := newClient(t, srv).SQL(context.Background(), "SELECT city, count(*) AS n FROM t GROUP BY 1", &topolab.SQLOptions{MaxRows: 500})
	if err != nil {
		t.Fatal(err)
	}
	if body["sql"] != "SELECT city, count(*) AS n FROM t GROUP BY 1" || body["maxRows"] != float64(500) {
		t.Fatalf("unexpected request body: %v", body)
	}
	if len(res.Columns) != 2 || res.RowCount != 2 || res.Truncated || res.ElapsedMs != 41.7 {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.Rows[0]["city"] != "Amsterdam" {
		t.Errorf("rows[0].city = %v", res.Rows[0]["city"])
	}
	if len(res.Datasets) != 1 || res.Datasets[0] != "business_professional_services_autocrew" {
		t.Errorf("datasets = %v", res.Datasets)
	}
}

func TestSQLOmitsMaxRowsWhenUnset(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		writeJSON(w, 200, map[string]any{"columns": []string{}, "rows": []any{}, "rowCount": 0})
	}))
	defer srv.Close()

	if _, err := newClient(t, srv).SQL(context.Background(), "SELECT 1", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["maxRows"]; ok {
		t.Errorf("maxRows sent when unset: %v", body)
	}
}

func TestSQLRequiresEntitlement(t *testing.T) {
	srv := serveFixture(t, 403, "error-403-sql-access.json")
	defer srv.Close()

	_, err := newClient(t, srv).SQL(context.Background(), "SELECT 1", nil)
	if !errors.Is(err, topolab.ErrAddonRequired) {
		t.Fatalf("want ErrAddonRequired, got %v", err)
	}
	var apiErr *topolab.Error
	if !errors.As(err, &apiErr) || apiErr.Addon != "sql-access" {
		t.Fatalf("addon = %+v", apiErr)
	}
}

func TestSQLQueryTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 408, map[string]any{"code": 408, "message": "Query exceeded the time limit"})
	}))
	defer srv.Close()

	_, err := newClient(t, srv).SQL(context.Background(), "SELECT pg_sleep(60)", nil)
	if !errors.Is(err, topolab.ErrQueryTimeout) {
		t.Fatalf("want ErrQueryTimeout, got %v", err)
	}
}
