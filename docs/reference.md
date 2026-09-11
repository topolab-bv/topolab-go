# API reference

The complete, always-current reference is generated from the source on
**[pkg.go.dev/github.com/topolab-bv/topolab-go](https://pkg.go.dev/github.com/topolab-bv/topolab-go)**.
This page is a quick map of the surface.

## Construction

```go
topolab.New(opts ...Option) (*Client, error)
```

| Option | Default |
|---|---|
| `WithAPIKey(string)` | `$TOPOLAB_API_KEY` |
| `WithBaseURL(string)` | resolved (see below) |
| `WithEnvironment(string)` | `production` |
| `WithTimeout(time.Duration)` | 60s |
| `WithMaxRetries(int)` | 3 |
| `WithHTTPClient(*http.Client)` | `&http.Client{Timeout: timeout}` |
| `WithUserAgent(string)` | `topolab-go/<version>` |

Base-URL precedence: `WithBaseURL` → `WithEnvironment` → `TOPOLAB_BASE_URL` →
`TOPOLAB_ENV` → production.

## Client

| Member | Returns |
|---|---|
| `c.Datasets.List(ctx, *ListOptions)` | `*DatasetPage, error` |
| `c.Datasets.Owned(ctx, *OwnedOptions)` | `*OwnedDatasets, error` |
| `c.Datasets.IterOwned(ctx, *IterOwnedOptions)` | `iter.Seq2[*OwnedDataset, error]` |
| `c.SQL(ctx, query, *SQLOptions)` | `*SQLResult, error` (Enterprise `sql-access`) |
| `c.Dataset(slug)` | `*Dataset` |
| `c.BaseURL()` | `string` |

## Dataset

| Method | Returns |
|---|---|
| `Metadata(ctx, locale)` | `*DatasetSummary, error` |
| `Sample(ctx, format)` | `[]byte, error` — `csv`/`json`/`geojson`/`kml` |
| `ToGeoJSON(ctx)` | `*FeatureCollection, error` (requires `api-access`) |
| `Download(ctx, path, format)` | `error` — `csv`/`json`/`geojson`/`kml`/`shp` |
| `Items(ctx, *ItemsOptions)` | `*FeatureCollection, error` |
| `IterItems(ctx, *IterOptions)` | `iter.Seq2[*Feature, error]` |
| `ItemsAll(ctx, *IterOptions)` | `*FeatureCollection, error` (concurrent) |
| `Archives(ctx)` | `[]Archive, error` — newest month first |
| `Archive(ctx, path, month, format)` | `error` — `latest`/`YYYY-MM`/`YYYY-MM-DD` |
| `Coordinates(ctx, *CoordinatesOptions)` | `*CoordinatePage, error` |

### `ItemsOptions`

```go
type ItemsOptions struct {
    BBox     []float64 // [minLon, minLat, maxLon, maxLat]
    Limit    int       // 1–1000, default 100
    Offset   int
    Category string
    City     string
    Country  string
}
```

### `IterOptions`

```go
type IterOptions struct {
    PageSize       int       // default 100
    TotalLimit     int       // 0 = all
    BBox           []float64
    Category       string
    City           string
    Country        string
    Sequential     bool      // ItemsAll: disable concurrent paging
    MaxConcurrency int       // default 6
}
```

### `OwnedOptions` / `IterOwnedOptions`

```go
type OwnedOptions struct {
    Limit  int // 1–200, 0 uses the server default of 50
    Offset int // >= 0
}

type IterOwnedOptions struct {
    PageSize   int // datasets per request (1–200, default 50)
    TotalLimit int // 0 = all
}
```

### `CoordinatesOptions`

```go
type CoordinatesOptions struct {
    Limit  int // 1–50000, 0 leaves it to the server (whole dataset, capped at 50000)
    Offset int // >= 0
}
```

### `SQLOptions`

```go
type SQLOptions struct {
    MaxRows int // 1–10000; 0 omits the field and uses the server default
}
```

### Archive months

`Archive` accepts `latest` (case-insensitive; `""` means the same), `YYYY-MM`,
and `YYYY-MM-DD`. The value is validated as a real calendar month or date before
the request is sent, so `2026-13`, `2026-07-99` and `2026-02-29` fail locally
with `ErrValidation` while `2024-02-29` passes. Server-side, a malformed or
impossible month is a 400; a well-formed month with no archive available is a
404, as are months outside your retention window and months that have not
started (deliberately indistinguishable). Team plans see a trailing 12 months of
archives; Enterprise and full-history add-ons see everything.

## Types

`DatasetSummary`, `DatasetPage`, `PageMeta`, `Feature`, `FeatureCollection`,
`Geometry` (with `Point() (lon, lat float64, ok bool)`), `OwnedDatasets`,
`OwnedDataset`, `OwnedLinks`, `Archive`, `CoordinatePage`, `CoordinateRow`,
`SQLResult`.

```go
type OwnedDataset struct {
    Table                  string   // dataset slug; pass to Client.Dataset
    Name                   string
    RecordCount            *int     // nil when unknown
    LatestArchiveMonth     *string  // "YYYY-MM"; nil when none is in range
    LatestArchiveFormats   []string
    ArchiveMonthsAvailable int
    Links                  OwnedLinks // Current/LatestArchive carry a literal {format}
}

type CoordinatePage struct {
    Rows     []CoordinateRow
    Total    int // X-Total-Count, falls back to len(Rows)
    Returned int // X-Returned-Count, falls back to len(Rows)
    Offset   int // X-Offset, falls back to 0
}
```

`CoordinateRow.Latitude` and `.Longitude` are **strings** — the API sends
fixed-precision decimals and the SDK does not coerce them; use
`CoordinateRow.Location.Point()` for numeric coordinates.

## Collections are addressed by slug

The OGC `collectionId` is the dataset's `table` slug (e.g. `nl-domino-poi`) — the
same value you pass to `Dataset()`. The client calls
`/v1/ogc/collections/{slug}/items` directly; there is no slug→uuid resolution.
