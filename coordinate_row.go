package topolab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The keys every coordinate row carries; each has a typed field on CoordinateRow.
const (
	rowKeyID        = "id"
	rowKeyLatitude  = "latitude"
	rowKeyLongitude = "longitude"
	rowKeyGeoType   = "geo_type"
	rowKeyGeometry  = "geometry"
	rowKeyName      = "location_name"
)

// CoordinateRow is one record of [Dataset.Coordinates] as a flat row: the free
// sample's row for the caller's tier plus the record's id.
//
// The keys every row carries are typed fields. Every other key is one of the
// dataset's columns and is kept in Columns, decoded as encoding/json decodes
// into any (numbers are float64, lists []any, objects map[string]any, null
// nil). The columns vary per dataset and tier; the dataset detail
// (GET /v1/dataset/{table}) describes them in its columns list. An empty value
// is null and a list (tags, category_path) is a JSON array. The advanced
// columns (such as phone, email, website, hours) and their credit columns are
// present only with the High-Value Data add-on for the dataset; a column the
// dataset keeps private is absent.
//
// Keys lists every key of the row in the order the API sent it: id first,
// latitude and longitude directly after location_name, geo_type after the
// dataset's columns, and geometry last. Value reads any key by name, so
//
//	for _, key := range row.Keys {
//		v, _ := row.Value(key)
//		fmt.Println(key, v)
//	}
//
// walks the whole row in that order. Marshalling a row writes the same JSON
// object back, keys in that order.
type CoordinateRow struct {
	// ID is the record's identifier; rows are ordered by it.
	ID string
	// Latitude and Longitude are WGS 84, nil for a record with no position.
	Latitude  *float64
	Longitude *float64
	// GeoType is the served geometry's type in upper case: "POINT" on this
	// route. Empty when the API sends null.
	GeoType string
	// Geometry is the record's point as a GeoJSON Point, coordinates
	// [longitude, latitude]; use Geometry.Point for the numbers.
	Geometry Geometry
	// Columns holds the dataset's own columns: every key other than id,
	// latitude, longitude, geo_type and geometry.
	Columns map[string]any
	// Keys lists every key of the row, the fixed ones included, in the order
	// the API sent them.
	Keys []string
}

// Value returns the value of one key of the row, fixed or column, and whether
// the row has it. A null Latitude, Longitude or GeoType reads as nil.
func (r CoordinateRow) Value(key string) (any, bool) {
	switch key {
	case rowKeyID:
		return r.ID, true
	case rowKeyLatitude:
		return floatOrNil(r.Latitude), true
	case rowKeyLongitude:
		return floatOrNil(r.Longitude), true
	case rowKeyGeoType:
		if r.GeoType == "" {
			return nil, true
		}
		return r.GeoType, true
	case rowKeyGeometry:
		return r.Geometry, true
	}
	v, ok := r.Columns[key]
	return v, ok
}

func floatOrNil(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

// UnmarshalJSON reads one flat row, keeping its key order in Keys. Latitude and
// longitude are read from a JSON number; decimal text, which older servers
// sent, is read as a number too.
func (r *CoordinateRow) UnmarshalJSON(data []byte) error {
	if string(bytes.TrimSpace(data)) == "null" {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil {
		return err
	} else if tok != json.Delim('{') {
		return fmt.Errorf("topolab: a coordinate row must be a JSON object, got %v", tok)
	}
	row := CoordinateRow{Columns: map[string]any{}}
	seen := map[string]bool{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		if !seen[key] {
			seen[key] = true
			row.Keys = append(row.Keys, key)
		}
		if err := row.set(key, raw); err != nil {
			return fmt.Errorf("topolab: coordinate row %q: %w", key, err)
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	*r = row
	return nil
}

func (r *CoordinateRow) set(key string, raw json.RawMessage) error {
	switch key {
	case rowKeyID:
		var id *string
		if err := json.Unmarshal(raw, &id); err != nil {
			return err
		}
		r.ID = ""
		if id != nil {
			r.ID = *id
		}
		return nil
	case rowKeyLatitude:
		f, err := coordinateNumber(raw)
		r.Latitude = f
		return err
	case rowKeyLongitude:
		f, err := coordinateNumber(raw)
		r.Longitude = f
		return err
	case rowKeyGeoType:
		var t *string
		if err := json.Unmarshal(raw, &t); err != nil {
			return err
		}
		r.GeoType = ""
		if t != nil {
			r.GeoType = *t
		}
		return nil
	case rowKeyGeometry:
		r.Geometry = Geometry{}
		return json.Unmarshal(raw, &r.Geometry)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	r.Columns[key] = v
	return nil
}

// coordinateNumber reads a latitude or longitude: a JSON number, null, or
// decimal text ("51.49638600", empty text being null).
func coordinateNumber(raw json.RawMessage) (*float64, error) {
	var f *float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("want a number, got %s", raw)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("want a number, got %q", s)
	}
	return &n, nil
}

// MarshalJSON writes the row as the flat JSON object the API sends, keys in
// the order of Keys. A key the row has that Keys does not list (a row built in
// code) follows the served order: id, location_name, latitude, longitude, the
// other columns by name, geo_type, geometry.
func (r CoordinateRow) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, key := range r.keyOrder() {
		v, _ := r.Value(key)
		k, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		val, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("topolab: coordinate row %q: %w", key, err)
		}
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(k)
		buf.WriteByte(':')
		buf.Write(val)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func (r CoordinateRow) keyOrder() []string {
	order := make([]string, 0, len(r.Columns)+5)
	seen := map[string]bool{}
	add := func(key string) {
		if seen[key] {
			return
		}
		if _, ok := r.Value(key); ok {
			seen[key] = true
			order = append(order, key)
		}
	}
	for _, key := range r.Keys {
		add(key)
	}
	add(rowKeyID)
	add(rowKeyName)
	add(rowKeyLatitude)
	add(rowKeyLongitude)
	rest := make([]string, 0, len(r.Columns))
	for key := range r.Columns {
		if !seen[key] {
			rest = append(rest, key)
		}
	}
	sort.Strings(rest)
	for _, key := range rest {
		add(key)
	}
	add(rowKeyGeoType)
	add(rowKeyGeometry)
	return order
}
