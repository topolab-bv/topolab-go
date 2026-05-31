# Topolab Go SDK

`topolab-go` is a lightweight, GeoJSON-first client over the [Topolab](https://topolab.nl)
dataset and geospatial API. It is context-aware, safe for concurrent use, and has
**zero third-party dependencies** — only the Go standard library.

!!! info "Reference & platform docs"
    The canonical Go API reference is on
    [pkg.go.dev](https://pkg.go.dev/github.com/topolab-bv/topolab-go) (generated
    from doc comments). This site is the narrative guide; the full platform
    documentation lives at [docs.topolab.nl](https://docs.topolab.nl).

## Install

```bash
go get github.com/topolab-bv/topolab-go
```

Requires Go 1.23+ (for the range-over-func iterator).

## Quickstart

```go
tl, err := topolab.New(topolab.WithAPIKey("tlb_prod_..."))
if err != nil {
    log.Fatal(err)
}
fc, err := tl.Dataset("nl-domino-poi").Items(context.Background(), &topolab.ItemsOptions{
    Limit: 100,
    BBox:  []float64{4.7, 52.2, 5.1, 52.5},
})
fmt.Printf("%d locations\n", len(fc.Features))
```

Your API key carries your scope and add-ons — spatial queries need `GIS_ACCESS`,
downloads need `API_ACCESS`, and data routes require an organization-scoped key.
Pass `WithAPIKey` or set `TOPOLAB_API_KEY` (`topolab.New()` reads it).

## Staging vs production

The client targets **production** (`https://api.topolab.nl`) by default. Switch
with `WithEnvironment("staging")` or `TOPOLAB_ENV=staging`. An explicit
`WithBaseURL` always wins. Precedence: `WithBaseURL` → `WithEnvironment` →
`TOPOLAB_BASE_URL` → `TOPOLAB_ENV` → production.
