// Command quickstart is a runnable Topolab Go SDK example.
//
//	TOPOLAB_API_KEY=tlb_prod_... go run ./examples/quickstart
package main

import (
	"context"
	"fmt"
	"log"

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
}
