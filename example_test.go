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

// The backend integration loop: page every licensed dataset and pull its newest
// monthly archive.
func ExampleDatasetsService_IterOwned() {
	tl, _ := topolab.New(topolab.WithAPIKey("tlb_prod_..."))
	ctx := context.Background()
	for d, err := range tl.Datasets.IterOwned(ctx, nil) {
		if err != nil {
			log.Fatal(err)
		}
		if d.LatestArchiveMonth == nil {
			continue // no archive inside this plan's retention window
		}
		if err := tl.Dataset(d.Table).Archive(ctx, "archives/"+d.Table+".zip", "latest", "geojson"); err != nil {
			log.Fatal(err)
		}
	}
}

// Run a read-only query across the datasets the organization licences.
func ExampleClient_SQL() {
	tl, _ := topolab.New(topolab.WithAPIKey("tlb_prod_..."))
	res, err := tl.SQL(context.Background(),
		"SELECT city, count(*) AS n FROM nl_domino_poi GROUP BY 1 ORDER BY n DESC",
		&topolab.SQLOptions{MaxRows: 100})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%d rows in %.1fms\n", res.RowCount, res.ElapsedMs)
}
