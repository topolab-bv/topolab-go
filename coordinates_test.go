package topolab_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	topolab "github.com/topolab-bv/topolab-go"
)

// objectKeys returns the keys of one JSON object in the order they appear.
func objectKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		t.Fatalf("not a JSON object: %v %v", tok, err)
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

// fixtureRows returns the coordinates fixture as raw rows, in order.
func fixtureRows(t *testing.T) []json.RawMessage {
	t.Helper()
	var rows []json.RawMessage
	if err := json.Unmarshal(ownedFixture(t, "coordinates.json"), &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func coordinatesFromFixture(t *testing.T) *topolab.CoordinatePage {
	t.Helper()
	srv := serveFixture(t, http.StatusOK, "coordinates.json")
	defer srv.Close()
	page, err := newClient(t, srv).Dataset("ds").Coordinates(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func TestCoordinateRowsAreFlat(t *testing.T) {
	row := coordinatesFromFixture(t).Rows[0]

	if row.ID != "02be5c77-fe43-44ca-878a-48cddddde98c" {
		t.Errorf("ID = %q", row.ID)
	}
	if row.Latitude == nil || *row.Latitude != 51.496386 || row.Longitude == nil || *row.Longitude != 3.655321 {
		t.Errorf("lat/lon = %v/%v, want 51.496386/3.655321", row.Latitude, row.Longitude)
	}
	if row.GeoType != "POINT" {
		t.Errorf("GeoType = %q, want POINT", row.GeoType)
	}
	if lon, lat, ok := row.Geometry.Point(); !ok || lon != 3.655321 || lat != 51.496386 {
		t.Errorf("Geometry.Point() = %v, %v, %v", lon, lat, ok)
	}
	// The dataset's columns sit on the row, not in a nested bag.
	if row.Columns["city"] != "Middelburg" || row.Columns["street"] != "Ampèreweg" {
		t.Errorf("columns = %v", row.Columns)
	}
	if tags, ok := row.Columns["tags"].([]any); !ok || len(tags) != 0 {
		t.Errorf("tags = %#v, want an empty list", row.Columns["tags"])
	}
	if v, ok := row.Columns["unit"]; !ok || v != nil {
		t.Errorf("unit = %v (present %v), want a present null", v, ok)
	}
	for _, gone := range []string{"metadata", "location", "datasetId", "dataset"} {
		if _, ok := row.Value(gone); ok {
			t.Errorf("row carries %q", gone)
		}
	}
	// The typed keys are not repeated among the columns.
	for _, fixed := range []string{"id", "latitude", "longitude", "geo_type", "geometry"} {
		if _, ok := row.Columns[fixed]; ok {
			t.Errorf("Columns repeats %q", fixed)
		}
	}
}

func TestCoordinateRowsKeepTheAPIKeyOrder(t *testing.T) {
	page := coordinatesFromFixture(t)
	for i, raw := range fixtureRows(t) {
		row := page.Rows[i]
		want := objectKeys(t, raw)
		if !reflect.DeepEqual(row.Keys, want) {
			t.Fatalf("row %d Keys = %v, want %v", i, row.Keys, want)
		}
		if row.Keys[0] != "id" || row.Keys[len(row.Keys)-1] != "geometry" {
			t.Errorf("row %d: id must be first and geometry last: %v", i, row.Keys)
		}
		for j, key := range row.Keys {
			if key == "location_name" && (row.Keys[j+1] != "latitude" || row.Keys[j+2] != "longitude") {
				t.Errorf("row %d: latitude and longitude must follow location_name: %v", i, row.Keys)
			}
			if _, ok := row.Value(key); !ok {
				t.Errorf("row %d: Value(%q) missing", i, key)
			}
		}
	}
}

func TestCoordinateRowRoundTrips(t *testing.T) {
	page := coordinatesFromFixture(t)
	for i, raw := range fixtureRows(t) {
		out, err := json.Marshal(page.Rows[i])
		if err != nil {
			t.Fatal(err)
		}
		if got, want := objectKeys(t, out), objectKeys(t, raw); !reflect.DeepEqual(got, want) {
			t.Errorf("row %d marshals keys %v, want %v", i, got, want)
		}
		var got, want map[string]any
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("row %d does not round-trip:\n got %v\nwant %v", i, got, want)
		}
	}
}

func TestCoordinateRowWithoutPosition(t *testing.T) {
	raw := []byte(`{"id":"r","location_name":"Unplaced","latitude":null,"longitude":null,"city":null,` +
		`"geo_type":"POINT","geometry":{"type":"Point","coordinates":[null,null]}}`)
	var row topolab.CoordinateRow
	if err := json.Unmarshal(raw, &row); err != nil {
		t.Fatal(err)
	}
	if row.Latitude != nil || row.Longitude != nil {
		t.Errorf("lat/lon = %v/%v, want nil", row.Latitude, row.Longitude)
	}
	if _, _, ok := row.Geometry.Point(); ok {
		t.Error("Point() of [null, null] must not report a position")
	}
	if v, ok := row.Value("latitude"); !ok || v != nil {
		t.Errorf("Value(latitude) = %v, %v; want nil, true", v, ok)
	}
	out, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != string(raw) {
		t.Errorf("round trip:\n got %s\nwant %s", out, raw)
	}
}

func TestCoordinateRowReadsDecimalText(t *testing.T) {
	// An older server sent latitude and longitude as decimal text.
	var row topolab.CoordinateRow
	if err := json.Unmarshal([]byte(`{"id":"r","latitude":"51.49638600","longitude":"3.65532100"}`), &row); err != nil {
		t.Fatal(err)
	}
	if row.Latitude == nil || *row.Latitude != 51.496386 || row.Longitude == nil || *row.Longitude != 3.655321 {
		t.Errorf("lat/lon = %v/%v", row.Latitude, row.Longitude)
	}
}

func TestCoordinateRowRejectsBadInput(t *testing.T) {
	for _, raw := range []string{`[1,2]`, `{"id":"r","latitude":"north"}`, `{"id":7}`, `{"id":"r","geo_type":1}`} {
		var row topolab.CoordinateRow
		if err := json.Unmarshal([]byte(raw), &row); err == nil {
			t.Errorf("%s: want an error", raw)
		}
	}
	// JSON null leaves the row as it was, as encoding/json does for a struct.
	row := topolab.CoordinateRow{ID: "kept"}
	if err := json.Unmarshal([]byte(`null`), &row); err != nil || row.ID != "kept" {
		t.Errorf("null: row %+v, err %v", row, err)
	}
}

func TestCoordinatesDecodeErrorIsServerKind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"r","latitude":"north"}]`))
	}))
	defer srv.Close()
	_, err := newClient(t, srv).Dataset("ds").Coordinates(context.Background(), nil)
	var tlErr *topolab.Error
	if err == nil || !errors.As(err, &tlErr) || tlErr.Kind != topolab.KindServer {
		t.Fatalf("want a KindServer decoding error, got %v", err)
	}
}

func TestBuiltRowMarshalsInTheServedOrder(t *testing.T) {
	lat, lon := 52.04, 5.66
	row := topolab.CoordinateRow{
		ID:        "r",
		Latitude:  &lat,
		Longitude: &lon,
		GeoType:   "POINT",
		Geometry:  topolab.Geometry{Type: "Point", Coordinates: json.RawMessage(`[5.66,52.04]`)},
		Columns:   map[string]any{"location_name": "Landal Ede", "city": "Ede"},
	}
	out, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"id":"r","location_name":"Landal Ede","latitude":52.04,"longitude":5.66,"city":"Ede",` +
		`"geo_type":"POINT","geometry":{"type":"Point","coordinates":[5.66,52.04]}}`
	if string(out) != want {
		t.Errorf("got  %s\nwant %s", out, want)
	}
}
