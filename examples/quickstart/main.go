// Command quickstart is a runnable Topolab Go SDK example.
//
//	TOPOLAB_API_KEY=tlb_prod_... go run ./examples/quickstart
package main

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	topolab "github.com/topolab-bv/topolab-go"
)

func main() {
	// Reads TOPOLAB_API_KEY from the environment.
	tl, err := topolab.New()
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()

	// Browse the catalog.
	page, err := tl.Datasets.List(ctx, &topolab.ListOptions{Country: "NL", Limit: 5})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("catalog: %d datasets\n", page.Meta.TotalItems)

	// Page features in an Amsterdam bounding box.
	ds := tl.Dataset("nl-domino-poi")
	fc, err := ds.Items(ctx, &topolab.ItemsOptions{Limit: 100, BBox: []float64{4.7, 52.2, 5.1, 52.5}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("first page: %d features (of %d matched)\n", len(fc.Features), fc.NumberMatched)

	// Fetch every feature concurrently.
	all, err := ds.ItemsAll(ctx, &topolab.IterOptions{PageSize: 500})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("all: %d features\n", len(all.Features))

	// The integration loop: everything the organization licences, newest
	// archive each. Needs no hard-coded slugs.
	for d, err := range tl.Datasets.IterOwned(ctx, &topolab.IterOwnedOptions{TotalLimit: 3}) {
		if err != nil {
			log.Fatal(err)
		}
		if d.LatestArchiveMonth == nil {
			fmt.Printf("%s: no archive in the retention window\n", d.Table)
			continue
		}
		out := filepath.Join("archives", d.Table+".zip")
		if err := tl.Dataset(d.Table).Archive(ctx, out, "latest", "geojson"); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: archive %s -> %s\n", d.Table, *d.LatestArchiveMonth, out)
	}

	// Coordinates come back as a bare array; the paging facts are in headers.
	coords, err := ds.Coordinates(ctx, &topolab.CoordinatesOptions{Limit: 100})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("coordinates: %d of %d rows\n", coords.Returned, coords.Total)
}
