# Guide

All calls take a `context.Context` for cancellation and deadlines.

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
