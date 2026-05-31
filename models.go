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
