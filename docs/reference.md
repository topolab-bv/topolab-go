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
| `c.Dataset(slug)` | `*Dataset` |
| `c.BaseURL()` | `string` |

## Dataset

| Method | Returns |
|---|---|
| `Metadata(ctx, locale)` | `*DatasetSummary, error` |
| `Sample(ctx, format)` | `[]byte, error` — `csv`/`json`/`geojson`/`kml` |
| `ToGeoJSON(ctx)` | `*FeatureCollection, error` (requires `API_ACCESS`) |
| `Download(ctx, path, format)` | `error` — `csv`/`json`/`geojson`/`kml`/`shp` |
| `Items(ctx, *ItemsOptions)` | `*FeatureCollection, error` |
| `IterItems(ctx, *IterOptions)` | `iter.Seq2[*Feature, error]` |
| `ItemsAll(ctx, *IterOptions)` | `*FeatureCollection, error` (concurrent) |

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

## Types

`DatasetSummary`, `DatasetPage`, `PageMeta`, `Feature`, `FeatureCollection`,
`Geometry` (with `Point() (lon, lat float64, ok bool)`).

## Collections are addressed by slug

The OGC `collectionId` is the dataset's `table` slug (e.g. `nl-domino-poi`) — the
same value you pass to `Dataset()`. The client calls
`/v1/ogc/collections/{slug}/items` directly; there is no slug→uuid resolution.
