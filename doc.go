// Package topolab is the official Go client for the Topolab dataset and
// geospatial API.
//
// It is a lightweight, GeoJSON-first wrapper over the REST and OGC API -
// Features endpoints: browse the catalog, read dataset metadata and samples,
// download bulk exports, and page through spatial features.
//
// # Quickstart
//
//	tl, err := topolab.New(topolab.WithAPIKey("tlb_prod_..."))
//	if err != nil {
//		log.Fatal(err)
//	}
//	fc, err := tl.Dataset("nl-domino-poi").Items(ctx, &topolab.ItemsOptions{
//		Limit: 100,
//		BBox:  []float64{4.7, 52.2, 5.1, 52.5},
//	})
//
// The API key carries your scope and add-ons: spatial queries need the
// gis-access add-on, downloads need api-access, and data routes require an
// organization-scoped key.
// The key is read from the WithAPIKey option or the TOPOLAB_API_KEY environment
// variable.
//
// # Pulling what you own
//
// The integration loop the SDK is built for needs no hard-coded slugs: list the
// datasets the organization licences, then pull each one's newest archive.
//
//	for d, err := range tl.Datasets.IterOwned(ctx, nil) {
//		if err != nil {
//			log.Fatal(err)
//		}
//		if err := tl.Dataset(d.Table).Archive(ctx, d.Table+".zip", "latest", "geojson"); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// [DatasetsService.Owned] is authoritative — it is filtered by the same licence
// check the download routes enforce, so everything it returns is downloadable.
// [Dataset.Archives] lists the months inside your retention window, and
// [Dataset.Archive] addresses one by "latest", "YYYY-MM" or "YYYY-MM-DD".
// [Client.SQL] runs a read-only query across the datasets you licence
// (Enterprise sql-access entitlement).
//
// # Environments
//
// The client targets production (https://api.topolab.nl) by default. Select
// staging with WithEnvironment("staging"); an explicit WithBaseURL wins.
// Resolution precedence, most-specific first: WithBaseURL, WithEnvironment,
// TOPOLAB_BASE_URL, TOPOLAB_ENV, production.
//
// # Errors
//
// Every API failure is a *[Error] carrying a [Kind]. Match a category with
// errors.Is against the exported sentinels (e.g. [ErrAddonRequired]), and read
// details (addon name, retry-after, credit balances) with errors.As:
//
//	var apiErr *topolab.Error
//	if errors.As(err, &apiErr) && apiErr.Kind == topolab.KindAddonRequired {
//		log.Printf("needs add-on: %s", apiErr.Addon)
//	}
package topolab

// Version is the SDK version, sent in the User-Agent header.
const Version = "0.1.0"
