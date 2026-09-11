package topolab

import "encoding/json"

// DatasetSummary is the catalog/metadata view of a dataset. Metadata holds the
// free-form per-dataset metadata object (title, description, fields, …) as
// decoded JSON; the comprehensive shape lives behind the dataset surface.
type DatasetSummary struct {
	ID       string         `json:"id"`
	Table    string         `json:"table"`
	Theme    string         `json:"theme,omitempty"`
	Country  string         `json:"country,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// PageMeta is the pagination envelope returned by the catalog endpoint.
type PageMeta struct {
	CurrentPage     int  `json:"currentPage"`
	ItemsPerPage    int  `json:"itemsPerPage"`
	TotalItems      int  `json:"totalItems"`
	TotalPages      int  `json:"totalPages"`
	HasPreviousPage bool `json:"hasPreviousPage"`
	HasNextPage     bool `json:"hasNextPage"`
}

// DatasetPage is one page of catalog results.
type DatasetPage struct {
	Data []DatasetSummary `json:"data"`
	Meta PageMeta         `json:"meta"`
}

// Geometry is a GeoJSON geometry. Type is the GeoJSON geometry type (e.g.
// "Point"); Coordinates is left as raw JSON so any geometry type round-trips
// losslessly without pulling in a geometry library. Use Point for the common
// case, or decode Coordinates yourself / with a package like paulmach/orb.
type Geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// Point decodes a Point geometry's coordinates into lon, lat. ok is false if the
// geometry is not a Point or cannot be decoded.
func (g Geometry) Point() (lon, lat float64, ok bool) {
	if g.Type != "Point" {
		return 0, 0, false
	}
	var c []float64
	if err := json.Unmarshal(g.Coordinates, &c); err != nil || len(c) < 2 {
		return 0, 0, false
	}
	return c[0], c[1], true
}

// Feature is a GeoJSON Feature.
type Feature struct {
	Type       string         `json:"type"`
	ID         any            `json:"id,omitempty"`
	Geometry   Geometry       `json:"geometry"`
	Properties map[string]any `json:"properties"`
}

// FeatureCollection is a GeoJSON FeatureCollection. NumberMatched /
// NumberReturned mirror the OGC API - Features paging fields.
type FeatureCollection struct {
	Type           string    `json:"type"`
	NumberMatched  int       `json:"numberMatched"`
	NumberReturned int       `json:"numberReturned"`
	Features       []Feature `json:"features"`
}

// OwnedDataset is one dataset the calling organization holds an active licence
// for, as returned by [DatasetsService.Owned]. Every entry is downloadable: the
// listing is filtered by the same licence check the download routes enforce.
//
// RecordCount, LatestArchiveMonth and Links.LatestArchive are pointers because
// the API returns null for them — no archive inside the retention window, or an
// unknown record count.
type OwnedDataset struct {
	// Table is the dataset slug: the value to pass to Client.Dataset and the
	// OGC collectionId.
	Table string `json:"table"`
	Name  string `json:"name"`

	RecordCount *int `json:"recordCount"`

	// LatestArchiveMonth is the newest archive month ("YYYY-MM") inside this
	// organization's retention window, or nil when none is in range.
	LatestArchiveMonth     *string  `json:"latestArchiveMonth"`
	LatestArchiveFormats   []string `json:"latestArchiveFormats,omitempty"`
	ArchiveMonthsAvailable int      `json:"archiveMonthsAvailable"`

	Links OwnedLinks `json:"links"`
}

// OwnedLinks are absolute URLs for one owned dataset, so an integration can walk
// from the listing to the data without building paths. Current and LatestArchive
// contain a literal "{format}" placeholder; LatestArchive is nil when no archive
// is in range.
type OwnedLinks struct {
	Current       string  `json:"current"`
	Archives      string  `json:"archives"`
	LatestArchive *string `json:"latestArchive"`
}

// OwnedDatasets is one page of licensed datasets. Total counts every licensed
// dataset, not the size of this page.
type OwnedDatasets struct {
	Items  []OwnedDataset `json:"items"`
	Total  int            `json:"total"`
	Limit  int            `json:"limit"`
	Offset int            `json:"offset"`
}

// Archive is one monthly snapshot of a dataset, as returned by
// [Dataset.Archives]. Month is "YYYY-MM"; ArchiveDate is the server-supplied
// date string and is kept verbatim.
type Archive struct {
	Month       string   `json:"month"`
	Formats     []string `json:"formats"`
	ArchiveDate string   `json:"archiveDate,omitempty"`
}

// CoordinateRow is one row of [Dataset.Coordinates]. Latitude and Longitude are
// returned by the API as strings (fixed-precision decimals) and are kept as
// strings so no precision is lost; use Location for numeric coordinates.
// Metadata is the free-form attribute bag — advanced-tier fields (email, phone,
// website, hours, services) appear only for organizations entitled to
// high-value data for the dataset.
type CoordinateRow struct {
	ID        string         `json:"id"`
	Location  Geometry       `json:"location"`
	Latitude  string         `json:"latitude"`
	Longitude string         `json:"longitude"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// CoordinatePage is one page of coordinate rows. The body of the response is a
// bare JSON array; Total, Returned and Offset come from the X-Total-Count,
// X-Returned-Count and X-Offset response headers. Missing or unparseable
// headers fall back to len(Rows) for Total and Returned, and 0 for Offset.
type CoordinatePage struct {
	Rows     []CoordinateRow
	Total    int // rows in the whole dataset, regardless of paging
	Returned int // rows in this page
	Offset   int // offset applied
}

// SQLResult is the outcome of [Client.SQL]. Rows are column-keyed maps; Datasets
// lists the licensed tables the query actually read, as resolved from the query
// plan.
type SQLResult struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	RowCount  int              `json:"rowCount"`
	Truncated bool             `json:"truncated"`
	ElapsedMs float64          `json:"elapsedMs"`
	Datasets  []string         `json:"datasets"`
}
