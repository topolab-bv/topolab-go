package topolab_test

import (
	"context"
	"fmt"
	"log"

	topolab "github.com/topolab-bv/topolab-go"
)

// Page features within an Amsterdam bounding box.
func ExampleClient_items() {
	tl, err := topolab.New(topolab.WithAPIKey("tlb_prod_..."))
	if err != nil {
		log.Fatal(err)
	}
	fc, err := tl.Dataset("nl-domino-poi").Items(context.Background(), &topolab.ItemsOptions{
		Limit: 100,
		BBox:  []float64{4.7, 52.2, 5.1, 52.5},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d locations\n", len(fc.Features))
}

// Stream every feature, paging transparently.
func ExampleDataset_IterItems() {
	tl, _ := topolab.New(topolab.WithAPIKey("tlb_prod_..."))
	ds := tl.Dataset("nl-domino-poi")
	for f, err := range ds.IterItems(context.Background(), &topolab.IterOptions{PageSize: 500}) {
		if err != nil {
			log.Fatal(err)
		}
		lon, lat, _ := f.Geometry.Point()
		_ = lon
		_ = lat
	}
}
