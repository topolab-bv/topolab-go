# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project adheres to
[Semantic Versioning](https://semver.org/).

## [0.1.0]

Initial release — the read-only v1 surface shared by the Topolab SDKs.

### Added
- `Client` with functional options (`WithAPIKey`, `WithBaseURL`,
  `WithEnvironment`, `WithTimeout`, `WithMaxRetries`, `WithHTTPClient`,
  `WithUserAgent`) and `TOPOLAB_API_KEY` / `TOPOLAB_BASE_URL` / `TOPOLAB_ENV`
  environment resolution (production default, staging switch).
- `Client.Datasets.List` catalog listing.
- `Dataset` handle: `Metadata`, `Sample`, `ToGeoJSON`, `Download` (atomic
  streaming), `Items`, `IterItems` (Go 1.23 range-over-func), and `ItemsAll`
  (concurrent paging).
- Zero-dependency GeoJSON types (`Feature`, `FeatureCollection`, `Geometry`
  with a `Point()` helper).
- Typed errors: a single `*Error` with a `Kind` and `errors.Is` sentinels;
  `X-API-Key` auth; retry/backoff for 429/5xx/network.
- SSRF base-URL validation (https-only, no userinfo, loopback http allowed).

[0.1.0]: https://github.com/topolab-bv/topolab-go/releases/tag/v0.1.0
