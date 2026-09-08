package topolab

import (
	"context"
	"iter"
	"net/url"
	"strconv"
)

// defaultOwnedPageSize matches the server default for GET /v1/dataset/owned.
const defaultOwnedPageSize = 50

// DatasetsService is the catalog surface, reachable as Client.Datasets.
type DatasetsService struct {
	t *transport
}

// ListOptions filters and paginates the catalog. The zero value lists the first
// page with server defaults.
type ListOptions struct {
	Page      int
	Limit     int
	Search    string
	Theme     string
	Country   string
	SortBy    string
	SortOrder string // "asc" | "desc"
}

func (o *ListOptions) query() url.Values {
	q := url.Values{}
	if o == nil {
		return q
	}
	if o.Page > 0 {
		q.Set("page", strconv.Itoa(o.Page))
	}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	setNonEmpty(q, "search", o.Search)
	setNonEmpty(q, "theme", o.Theme)
	setNonEmpty(q, "country", o.Country)
	setNonEmpty(q, "sortBy", o.SortBy)
	setNonEmpty(q, "sortOrder", o.SortOrder)
	return q
}

// List returns one page of the dataset catalog.
func (s *DatasetsService) List(ctx context.Context, opts *ListOptions) (*DatasetPage, error) {
	var page DatasetPage
	if err := s.t.getJSON(ctx, "/v1/dataset/all", opts.query(), &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// OwnedOptions paginates the owned-datasets listing. The zero value requests the
// first page with the server default (50 per page).
type OwnedOptions struct {
	Limit  int // 1–200, 0 uses the server default of 50
	Offset int // >= 0
}

func (o *OwnedOptions) query() (url.Values, error) {
	q := url.Values{}
	if o == nil {
		return q, nil
	}
	if o.Limit < 0 || o.Limit > 200 {
		return nil, &Error{Kind: KindValidation, Message: "owned limit must be between 1 and 200"}
	}
	if o.Offset < 0 {
		return nil, &Error{Kind: KindValidation, Message: "owned offset must not be negative"}
	}
	if o.Limit > 0 {
		q.Set("limit", strconv.Itoa(o.Limit))
	}
	if o.Offset > 0 {
		q.Set("offset", strconv.Itoa(o.Offset))
	}
	return q, nil
}

// Owned returns one page of the datasets the organization holds an active
// licence for — the entry point for a backend integration, since every entry is
// downloadable and carries absolute links to its current file and archives.
// OwnedDatasets.Total counts every licensed dataset, not the page.
//
// Requires an organization-scoped key with the api-access add-on.
func (s *DatasetsService) Owned(ctx context.Context, opts *OwnedOptions) (*OwnedDatasets, error) {
	q, err := opts.query()
	if err != nil {
		return nil, err
	}
	var page OwnedDatasets
	if err := s.t.getJSON(ctx, "/v1/dataset/owned", q, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

// IterOwnedOptions control auto-pagination for IterOwned.
type IterOwnedOptions struct {
	PageSize   int // datasets per request (1–200, default 50)
	TotalLimit int // stop after this many datasets (0 = all)
}

// IterOwned returns an iterator over every licensed dataset, paging by offset
// until the reported total is reached. Iteration stops on the first error
// (yielded with a nil dataset) or when the caller breaks. Requires Go 1.23+.
//
//	for d, err := range tl.Datasets.IterOwned(ctx, nil) {
//		if err != nil {
//			return err
//		}
//		if err := tl.Dataset(d.Table).Archive(ctx, d.Table+".zip", "latest", "geojson"); err != nil {
//			return err
//		}
//	}
func (s *DatasetsService) IterOwned(ctx context.Context, opts *IterOwnedOptions) iter.Seq2[*OwnedDataset, error] {
	pageSize := defaultOwnedPageSize
	totalLimit := 0
	if opts != nil {
		if opts.PageSize > 0 {
			pageSize = opts.PageSize
		}
		totalLimit = opts.TotalLimit
	}
	return func(yield func(*OwnedDataset, error) bool) {
		offset, yielded := 0, 0
		for {
			page, err := s.Owned(ctx, &OwnedOptions{Limit: pageSize, Offset: offset})
			if err != nil {
				yield(nil, err)
				return
			}
			if len(page.Items) == 0 {
				return
			}
			for i := range page.Items {
				if !yield(&page.Items[i], nil) {
					return
				}
				yielded++
				if totalLimit > 0 && yielded >= totalLimit {
					return
				}
			}
			// Advance by what the server actually returned, so a short page
			// neither repeats nor skips an entry.
			offset += len(page.Items)
			if page.Total > 0 && offset >= page.Total {
				return
			}
			if len(page.Items) < pageSize {
				return
			}
		}
	}
}

func setNonEmpty(q url.Values, key, val string) {
	if val != "" {
		q.Set(key, val)
	}
}
