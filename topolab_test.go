package topolab_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	topolab "github.com/topolab-bv/topolab-go"
)

const apiKey = "tlb_test_key"

func newClient(t *testing.T, srv *httptest.Server) *topolab.Client {
	t.Helper()
	c, err := topolab.New(topolab.WithAPIKey(apiKey), topolab.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewRequiresKey(t *testing.T) {
	os.Unsetenv("TOPOLAB_API_KEY")
	_, err := topolab.New(topolab.WithBaseURL("https://api.topolab.nl"))
	if !errors.Is(err, topolab.ErrConfiguration) {
		t.Fatalf("want ErrConfiguration, got %v", err)
	}
}

func TestEnvironmentResolution(t *testing.T) {
	os.Unsetenv("TOPOLAB_BASE_URL")
	os.Unsetenv("TOPOLAB_ENV")
	cases := []struct {
		name string
		opts []topolab.Option
		want string
	}{
		{"default production", nil, "https://api.topolab.nl"},
		{"staging", []topolab.Option{topolab.WithEnvironment("staging")}, "https://api-staging.topolab.nl"},
		{"explicit base wins", []topolab.Option{topolab.WithEnvironment("staging"), topolab.WithBaseURL("https://self.example/api")}, "https://self.example/api"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := append([]topolab.Option{topolab.WithAPIKey(apiKey)}, tc.opts...)
			c, err := topolab.New(opts...)
			if err != nil {
				t.Fatal(err)
			}
			if c.BaseURL() != tc.want {
				t.Errorf("BaseURL = %q, want %q", c.BaseURL(), tc.want)
			}
		})
	}
}

func TestUnknownEnvironment(t *testing.T) {
	_, err := topolab.New(topolab.WithAPIKey(apiKey), topolab.WithEnvironment("dev"))
	if !errors.Is(err, topolab.ErrConfiguration) {
		t.Fatalf("want ErrConfiguration, got %v", err)
	}
}

func TestBaseURLValidation(t *testing.T) {
	bad := []string{"http://evil.example/api", "https://user:pass@evil.example", "ftp://api.topolab.nl"}
	for _, u := range bad {
		if _, err := topolab.New(topolab.WithAPIKey(apiKey), topolab.WithBaseURL(u)); !errors.Is(err, topolab.ErrConfiguration) {
			t.Errorf("%s: want ErrConfiguration, got %v", u, err)
		}
	}
	// loopback http is allowed
	if _, err := topolab.New(topolab.WithAPIKey(apiKey), topolab.WithBaseURL("http://127.0.0.1:8080")); err != nil {
		t.Errorf("loopback http rejected: %v", err)
	}
}

func TestListCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-API-Key"); got != apiKey {
			t.Errorf("X-API-Key = %q", got)
		}
		if r.URL.Path != "/v1/dataset/all" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeJSON(w, 200, map[string]any{
			"data": []map[string]any{{"id": "uuid-1", "table": "nl-domino-poi", "theme": "retail"}},
			"meta": map[string]any{"totalItems": 1, "currentPage": 1},
		})
	}))
	defer srv.Close()

	page, err := newClient(t, srv).Datasets.List(context.Background(), &topolab.ListOptions{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Meta.TotalItems != 1 || len(page.Data) != 1 || page.Data[0].Table != "nl-domino-poi" {
		t.Fatalf("unexpected page: %+v", page)
	}
}

func TestItemsSlugDirect(t *testing.T) {
	var mdHits, itemHits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/v1/dataset/"):
			atomic.AddInt32(&mdHits, 1)
			writeJSON(w, 200, map[string]any{"id": "uuid-1", "table": "nl-domino-poi"})
		case r.URL.Path == "/v1/ogc/collections/nl-domino-poi/items":
			atomic.AddInt32(&itemHits, 1)
			if got := r.URL.Query().Get("bbox"); got != "4.7,52.2,5.1,52.5" {
				t.Errorf("bbox = %q", got)
			}
			writeJSON(w, 200, itemsFixture(2, 2))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	fc, err := newClient(t, srv).Dataset("nl-domino-poi").Items(context.Background(), &topolab.ItemsOptions{
		Limit: 100, BBox: []float64{4.7, 52.2, 5.1, 52.5},
	})
	if err != nil {
		t.Fatal(err)
	}
	if fc.Type != "FeatureCollection" || len(fc.Features) != 2 {
		t.Fatalf("unexpected fc: %+v", fc)
	}
	if mdHits != 0 {
		t.Errorf("metadata round-trip happened (%d): items must be slug-direct", mdHits)
	}
	// geometry helper
	lon, lat, ok := fc.Features[0].Geometry.Point()
	if !ok || lon == 0 || lat == 0 {
		t.Errorf("Point() = %v,%v,%v", lon, lat, ok)
	}
}

func TestIterItemsPaginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		off := r.URL.Query().Get("offset")
		if off == "" || off == "0" {
			writeJSON(w, 200, itemsFixture(2, 4)) // full page of 2, 4 matched
		} else {
			writeJSON(w, 200, itemsFixture(2, 4)) // second full page
		}
	}))
	defer srv.Close()

	var n int
	for f, err := range newClient(t, srv).Dataset("nl-domino-poi").IterItems(context.Background(), &topolab.IterOptions{PageSize: 2, TotalLimit: 3}) {
		if err != nil {
			t.Fatal(err)
		}
		if f == nil {
			t.Fatal("nil feature without error")
		}
		n++
	}
	if n != 3 {
		t.Fatalf("iterated %d features, want 3 (TotalLimit)", n)
	}
}

func TestItemsAllParallel(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		off := r.URL.Query().Get("offset")
		if off == "" || off == "0" {
			writeJSON(w, 200, itemsFixture(2, 4)) // numberMatched=4
		} else {
			writeJSON(w, 200, itemsFixture(2, 4))
		}
	}))
	defer srv.Close()

	fc, err := newClient(t, srv).Dataset("nl-domino-poi").ItemsAll(context.Background(), &topolab.IterOptions{PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(fc.Features) != 4 {
		t.Fatalf("got %d features, want 4", len(fc.Features))
	}
	if fc.NumberReturned != 4 {
		t.Errorf("NumberReturned = %d, want 4", fc.NumberReturned)
	}
}

func TestAddonRequiredError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 403, map[string]any{"statusCode": 403, "message": "This endpoint requires the API_ACCESS add-on", "error": "Forbidden"})
	}))
	defer srv.Close()

	_, err := newClient(t, srv).Dataset("nl-domino-poi").ToGeoJSON(context.Background())
	if !errors.Is(err, topolab.ErrAddonRequired) {
		t.Fatalf("want ErrAddonRequired, got %v", err)
	}
	var apiErr *topolab.Error
	if !errors.As(err, &apiErr) || apiErr.Addon != "API_ACCESS" {
		t.Fatalf("addon not parsed: %+v", apiErr)
	}
}

func TestRateLimitRetries(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			writeJSON(w, 429, map[string]any{"statusCode": 429, "message": "slow down", "retryAfter": 0})
			return
		}
		writeJSON(w, 200, map[string]any{"id": "uuid-1", "table": "nl-domino-poi"})
	}))
	defer srv.Close()

	c, _ := topolab.New(topolab.WithAPIKey(apiKey), topolab.WithBaseURL(srv.URL), topolab.WithMaxRetries(2))
	if _, err := c.Dataset("nl-domino-poi").Metadata(context.Background(), ""); err != nil {
		t.Fatalf("retry did not recover: %v", err)
	}
	if hits != 2 {
		t.Errorf("hits = %d, want 2 (1 retry)", hits)
	}
}

func TestNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]any{"statusCode": 404, "message": "not found"})
	}))
	defer srv.Close()
	_, err := newClient(t, srv).Dataset("nope").Metadata(context.Background(), "")
	if !errors.Is(err, topolab.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDownloadStreamsToDisk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/geo+json")
		_ = json.NewEncoder(w).Encode(itemsFixture(2, 2))
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "nested", "out.geojson")
	if err := newClient(t, srv).Dataset("nl-domino-poi").Download(context.Background(), out, "geojson"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("output missing: %v", err)
	}
	if !strings.Contains(string(data), "FeatureCollection") {
		t.Errorf("unexpected file contents")
	}
}

func TestSampleFormatValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("should not call server") }))
	defer srv.Close()
	if _, err := newClient(t, srv).Dataset("x").Sample(context.Background(), "xlsx"); !errors.Is(err, topolab.ErrValidation) {
		t.Fatalf("want ErrValidation, got %v", err)
	}
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func itemsFixture(returned, matched int) map[string]any {
	feats := make([]map[string]any, returned)
	for i := range feats {
		feats[i] = map[string]any{
			"type":       "Feature",
			"id":         i + 1,
			"geometry":   map[string]any{"type": "Point", "coordinates": []float64{4.9 + float64(i)/100, 52.37}},
			"properties": map[string]any{"city": "Amsterdam"},
		}
	}
	return map[string]any{"type": "FeatureCollection", "numberReturned": returned, "numberMatched": matched, "features": feats}
}
