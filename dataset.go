package topolab

import (
	"context"
	"io"
	"iter"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

var (
	sampleFormats = map[string]bool{"csv": true, "json": true, "geojson": true, "kml": true}
	bulkFormats   = map[string]bool{"csv": true, "json": true, "geojson": true, "kml": true, "shp": true}
)

// Dataset is a lazy handle for a single dataset, created via Client.Dataset. The
// OGC collectionId is the dataset slug, so the spatial methods address
// /v1/ogc/collections/{slug}/items directly — there is no metadata round-trip.
type Dataset struct {
	t    *transport
	Slug string
}

// Metadata returns the dataset's metadata. Pass an empty locale for the default.
func (d *Dataset) Metadata(ctx context.Context, locale string) (*DatasetSummary, error) {
	q := url.Values{}
	setNonEmpty(q, "locale", locale)
	var s DatasetSummary
	if err := d.t.getJSON(ctx, "/v1/dataset/"+d.Slug, q, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Sample returns a free preview of the dataset in the given format (one of
// csv, json, geojson, kml; empty defaults to geojson) as raw bytes.
func (d *Dataset) Sample(ctx context.Context, format string) ([]byte, error) {
	if format == "" {
		format = "geojson"
	}
	if !sampleFormats[format] {
		return nil, &Error{Kind: KindValidation, Message: "sample format must be one of csv, json, geojson, kml"}
	}
	resp, err := d.t.do(ctx, "/v1/dataset/"+d.Slug+"/sample/"+format, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// ToGeoJSON returns the full dataset as a FeatureCollection. Requires the
// API_ACCESS add-on and consumes credits.
func (d *Dataset) ToGeoJSON(ctx context.Context) (*FeatureCollection, error) {
	var fc FeatureCollection
	if err := d.t.getJSON(ctx, "/v1/dataset/"+d.Slug+"/files/geojson", nil, &fc); err != nil {
		return nil, err
	}
	return &fc, nil
}

// Download streams a bulk export to path (format one of csv, json, geojson,
// kml, shp; empty defaults to geojson). The destination directory is created if
// needed; the download streams to a temp file and is renamed atomically, so an
// interrupted transfer never leaves a truncated file at path.
func (d *Dataset) Download(ctx context.Context, path, format string) error {
	if format == "" {
		format = "geojson"
	}
	if !bulkFormats[format] {
		return &Error{Kind: KindValidation, Message: "download format must be one of csv, json, geojson, kml, shp"}
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return &Error{Kind: KindConnection, Message: err.Error()}
		}
	}
	resp, err := d.t.do(ctx, "/v1/dataset/"+d.Slug+"/files/"+format, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp(filepath.Dir(path), ".topolab-*.part")
	if err != nil {
		return &Error{Kind: KindConnection, Message: err.Error()}
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return &Error{Kind: KindConnection, Message: err.Error()}
	}
	if err := tmp.Close(); err != nil {
		return &Error{Kind: KindConnection, Message: err.Error()}
	}
	if err := os.Rename(tmpName, path); err != nil {
		return &Error{Kind: KindConnection, Message: err.Error()}
	}
	return nil
}

// ItemsOptions are query parameters for a single page of OGC features. BBox, if
// set, is [minLon, minLat, maxLon, maxLat].
type ItemsOptions struct {
	BBox     []float64
	Limit    int
	Offset   int
	Category string
	City     string
	Country  string
}

func (o *ItemsOptions) query() url.Values {
	q := url.Values{}
	limit := 100
	if o != nil && o.Limit > 0 {
		limit = o.Limit
	}
	q.Set("limit", strconv.Itoa(limit))
	if o == nil {
		return q
	}
	if o.Offset > 0 {
		q.Set("offset", strconv.Itoa(o.Offset))
	}
	if len(o.BBox) > 0 {
		q.Set("bbox", joinFloats(o.BBox))
	}
	setNonEmpty(q, "category", o.Category)
	setNonEmpty(q, "city", o.City)
	setNonEmpty(q, "country", o.Country)
	return q
}

// Items returns a single page of OGC features for the collection.
func (d *Dataset) Items(ctx context.Context, opts *ItemsOptions) (*FeatureCollection, error) {
	var fc FeatureCollection
	if err := d.t.getJSON(ctx, d.itemsPath(), opts.query(), &fc); err != nil {
		return nil, err
	}
	return &fc, nil
}

// IterOptions control auto-pagination for IterItems and ItemsAll.
type IterOptions struct {
	PageSize   int // features per request (default 100)
	TotalLimit int // stop after this many features (0 = all)
	BBox       []float64
	Category   string
	City       string
	Country    string

	// Sequential disables concurrent page fetching in ItemsAll. By default
	// (zero value) ItemsAll fetches the remaining pages concurrently;
	// MaxConcurrency bounds simultaneous requests (default 6). IterItems always
	// pages sequentially to preserve order, regardless of this field.
	Sequential     bool
	MaxConcurrency int
}

func (o *IterOptions) pageItemsOptions(offset int) *ItemsOptions {
	io := &ItemsOptions{Limit: 100, Offset: offset}
	if o != nil {
		if o.PageSize > 0 {
			io.Limit = o.PageSize
		}
		io.BBox, io.Category, io.City, io.Country = o.BBox, o.Category, o.City, o.Country
	}
	return io
}

// IterItems returns an iterator over every matching feature, paging
// sequentially and transparently. Iteration stops on the first error (yielded
// with a nil feature) or when the caller breaks. Requires Go 1.23+.
//
//	for f, err := range ds.IterItems(ctx, nil) {
//		if err != nil { return err }
//		_ = f
//	}
func (d *Dataset) IterItems(ctx context.Context, opts *IterOptions) iter.Seq2[*Feature, error] {
	pageSize := 100
	if opts != nil && opts.PageSize > 0 {
		pageSize = opts.PageSize
	}
	total := 0
	if opts != nil {
		total = opts.TotalLimit
	}
	return func(yield func(*Feature, error) bool) {
		offset, yielded := 0, 0
		for {
			fc, err := d.Items(ctx, opts.pageItemsOptions(offset))
			if err != nil {
				yield(nil, err)
				return
			}
			if len(fc.Features) == 0 {
				return
			}
			for i := range fc.Features {
				if !yield(&fc.Features[i], nil) {
					return
				}
				yielded++
				if total > 0 && yielded >= total {
					return
				}
			}
			if len(fc.Features) < pageSize {
				return
			}
			offset += pageSize
		}
	}
}

// ItemsAll fetches every matching feature into one FeatureCollection. It reads
// the first page (which reports numberMatched), then fetches the remaining pages
// concurrently unless IterOptions.Sequential is set. Honours TotalLimit.
func (d *Dataset) ItemsAll(ctx context.Context, opts *IterOptions) (*FeatureCollection, error) {
	pageSize := 100
	parallel := true
	maxConc := 6
	total := 0
	if opts != nil {
		if opts.PageSize > 0 {
			pageSize = opts.PageSize
		}
		parallel = !opts.Sequential
		if opts.MaxConcurrency > 0 {
			maxConc = opts.MaxConcurrency
		}
		total = opts.TotalLimit
	}

	first, err := d.Items(ctx, opts.pageItemsOptions(0))
	if err != nil {
		return nil, err
	}
	out := &FeatureCollection{Type: "FeatureCollection", NumberMatched: first.NumberMatched}
	out.Features = append(out.Features, first.Features...)
	if len(first.Features) < pageSize {
		return trim(out, total), nil
	}

	target := first.NumberMatched
	if total > 0 && (target == 0 || total < target) {
		target = total
	}

	// Sequential when requested, or when we can't tell how many pages remain.
	if !parallel || target <= 0 {
		offset := pageSize
		for {
			if total > 0 && len(out.Features) >= total {
				break
			}
			page, err := d.Items(ctx, opts.pageItemsOptions(offset))
			if err != nil {
				return nil, err
			}
			if len(page.Features) == 0 {
				break
			}
			out.Features = append(out.Features, page.Features...)
			if len(page.Features) < pageSize {
				break
			}
			offset += pageSize
		}
		return trim(out, total), nil
	}

	// Parallel: enumerate remaining offsets and fetch concurrently.
	var offsets []int
	for off := pageSize; off < target; off += pageSize {
		offsets = append(offsets, off)
	}
	pages := make([][]Feature, len(offsets))
	sem := make(chan struct{}, maxConc)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i, off := range offsets {
		wg.Add(1)
		go func(i, off int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			page, err := d.Items(cctx, opts.pageItemsOptions(off))
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
				return
			}
			pages[i] = page.Features
		}(i, off)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	for _, p := range pages {
		out.Features = append(out.Features, p...)
	}
	return trim(out, total), nil
}

func (d *Dataset) itemsPath() string {
	return "/v1/ogc/collections/" + d.Slug + "/items"
}

func trim(fc *FeatureCollection, total int) *FeatureCollection {
	if total > 0 && len(fc.Features) > total {
		fc.Features = fc.Features[:total]
	}
	fc.NumberReturned = len(fc.Features)
	return fc
}

func joinFloats(f []float64) string {
	parts := make([]string, len(f))
	for i, v := range f {
		parts[i] = strconv.FormatFloat(v, 'g', -1, 64)
	}
	return strings.Join(parts, ",")
}
