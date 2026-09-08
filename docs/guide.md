# Guide

All calls take a `context.Context` for cancellation and deadlines.

## Pull what you own

This is the loop the SDK exists for: list the datasets your organization
licences, then pull each one's newest monthly archive. No hard-coded slugs.

```go
for d, err := range tl.Datasets.IterOwned(ctx, nil) {
    if err != nil {
        return err
    }
    if d.LatestArchiveMonth == nil {
        continue // nothing inside this plan's retention window
    }
    if err := tl.Dataset(d.Table).Archive(ctx, "archives/"+d.Table+".zip", "latest", "geojson"); err != nil {
        return err
    }
}
```

`Owned` is authoritative: it is filtered by the same licence check the download
routes enforce, so everything it returns is downloadable. Each entry also carries
absolute `Links` (`Current`, `Archives`, `LatestArchive`), so an integration can
follow URLs instead of building paths. `Current` and `LatestArchive` contain a
literal `{format}` placeholder; `LatestArchive`, `LatestArchiveMonth` and
`RecordCount` are pointers because the API returns `null` for them.

`IterOwned` pages by offset until the reported total is reached, and stops on a
short page. One page at a time is also available:

```go
page, _ := tl.Datasets.Owned(ctx, &topolab.OwnedOptions{Limit: 200, Offset: 0})
fmt.Println(page.Total) // every licensed dataset, not the size of this page
```

`Limit` is 1–200 (server default 50) and `Offset` must not be negative; both are
checked before the request leaves the process.

### Archives

```go
archives, _ := ds.Archives(ctx)          // newest month first
fmt.Println(archives[0].Month)           // "2026-07"
fmt.Println(archives[0].Formats)         // [csv geojson json kml shp]

_ = ds.Archive(ctx, "ds-latest.zip", "latest", "geojson")
_ = ds.Archive(ctx, "ds-2026-07.zip", "2026-07", "csv")
_ = ds.Archive(ctx, "ds-2026-07.zip", "2026-07-15", "csv") // the month containing that date
```

`month` accepts three forms:

| Value | Meaning |
|---|---|
| `latest` (case-insensitive; also `""`) | newest archive inside your retention window |
| `YYYY-MM` | that month |
| `YYYY-MM-DD` | the month containing that date |

The month is validated as a **real calendar value** before the request is sent,
so `2026-13`, `2026-07-99` and `2026-02-29` fail locally with `ErrValidation`
(`2024-02-29` is a leap day and passes), saving a round trip and a credit.

Server-side the same rule applies: a malformed or impossible month is a **400**
(`ErrValidation`), while a well-formed month with **no archive available** is a
**404** (`ErrNotFound`). Months outside your retention window and months that
have not started both return 404 and are deliberately indistinguishable, so the
response never reveals an archive you cannot access.

Retention: Team plans see a trailing 12 months; Enterprise and full-history
add-ons see everything. `Archives` already reflects your window, so it never
lists a month that would 404. Listing archives is free; downloading one consumes
credits and needs the `archived-data` add-on.

### Coordinates

`Coordinates` returns rows with their attribute bag. The API answers with a bare
JSON array and reports paging in the `X-Total-Count`, `X-Returned-Count` and
`X-Offset` headers, which the SDK folds into the returned page:

```go
page, _ := ds.Coordinates(ctx, &topolab.CoordinatesOptions{Limit: 1000, Offset: 0})
fmt.Println(page.Returned, "of", page.Total)

row := page.Rows[0]
lon, lat, _ := row.Location.Point() // numeric geometry
_ = row.Latitude                    // "51.49638600" — the API sends strings
_ = row.Metadata["city"]
```

`Latitude` and `Longitude` are **strings**, exactly as the API sends them, so no
precision is lost; use `Location` when you want numbers. Missing or unparseable
headers are not an error: `Total`/`Returned` fall back to the number of rows
decoded and `Offset` to `0`. `Limit` is 1–50000 (omit it to take the whole
dataset in one response, capped at 50000 rows).

### SQL (Enterprise)

```go
res, err := tl.SQL(ctx, "SELECT city, count(*) AS n FROM nl_domino_poi GROUP BY 1", &topolab.SQLOptions{MaxRows: 100})
fmt.Println(res.Columns, res.RowCount, res.Truncated, res.ElapsedMs, res.Datasets)
```

`SQL` lives on the client, not on a dataset handle, because a query may span
several datasets. It requires the `sql-access` entitlement, part of the
Enterprise plan and not sold separately; without it the call fails with
`ErrAddonRequired`. Queries are a single read-only `SELECT` (or `WITH`) — no
DDL/DML, no system catalogues — and every relation the planner resolves must be
a dataset you licence. A query that outruns the server statement timeout fails
with `ErrQueryTimeout` (408).

## Browse the catalog

```go
page, err := tl.Datasets.List(ctx, &topolab.ListOptions{Country: "NL", Limit: 10})
for _, d := range page.Data {
    fmt.Println(d.Table, d.Theme)
}
```

## Dataset metadata and samples

```go
ds := tl.Dataset("nl-domino-poi")
meta, _ := ds.Metadata(ctx, "")            // pass a locale ("en"/"nl") or ""
sample, _ := ds.Sample(ctx, "geojson")     // raw bytes; csv/json/geojson/kml
```

## Query features in an area (spatial, paged)

`Items` addresses the collection by slug directly — the OGC `collectionId` **is**
the dataset slug, so there is no metadata round-trip.

```go
fc, _ := ds.Items(ctx, &topolab.ItemsOptions{
    Limit: 100, BBox: []float64{4.7, 52.2, 5.1, 52.5},
})
```

### Stream every feature

`IterItems` returns a Go 1.23 `iter.Seq2[*Feature, error]`, pages sequentially,
and stops on the first error or when you `break`:

```go
for f, err := range ds.IterItems(ctx, &topolab.IterOptions{PageSize: 500}) {
    if err != nil {
        return err
    }
    lon, lat, ok := f.Geometry.Point()
    _ = lon; _ = lat; _ = ok
}
```

### Fetch everything (concurrent)

`ItemsAll` reads the first page (which reports `numberMatched`), then fetches the
remaining pages **concurrently**:

```go
all, _ := ds.ItemsAll(ctx, &topolab.IterOptions{PageSize: 500})              // concurrent
seq, _ := ds.ItemsAll(ctx, &topolab.IterOptions{PageSize: 500, Sequential: true})
capped, _ := ds.ItemsAll(ctx, &topolab.IterOptions{PageSize: 500, TotalLimit: 2000})
```

Bound concurrency with `MaxConcurrency` (default 6).

## Pull a whole dataset (bulk)

```go
fc, _ := ds.ToGeoJSON(ctx)                          // FeatureCollection
_ = ds.Download(ctx, "exports/dominos-nl.geojson", "geojson")
```

`Download` creates the destination directory and streams to a temp file that is
renamed atomically, so an interrupted transfer never leaves a truncated file.

## Working with geometry

Geometry coordinates are kept as raw JSON so any geometry type round-trips with no
dependency. For the common Point case:

```go
lon, lat, ok := feature.Geometry.Point()
```

For richer geometry handling, decode `Geometry.Coordinates` yourself or with a
package such as [`paulmach/orb`](https://github.com/paulmach/orb).
