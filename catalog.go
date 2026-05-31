package topolab

import (
	"context"
	"net/url"
	"strconv"
)

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

func setNonEmpty(q url.Values, key, val string) {
	if val != "" {
		q.Set(key, val)
	}
}
