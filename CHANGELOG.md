# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project adheres to
[Semantic Versioning](https://semver.org/).

## [0.2.1]

### Changed
- 429 handling follows the engine's `RATE_LIMIT_EXCEEDED` and
  `USAGE_QUOTA_EXCEEDED` codes. The wait is read from the `Retry-After` header,
  then `details.retryAfter`, then the older top-level `retryAfter`; before, the
  header came last and `details` was not read. `Error.RetryAfter` and the retry
  delay use the same rule.
- `Retry-After` is also read as an HTTP date (RFC 9110). Negative or
  unparseable values are ignored.
- `docs/errors.md` lists both 429 codes and their `Details`.

## [0.2.0]

### Changed
- **`Error` exposes `Code` and `Details`.** The engine's error envelope now
  carries a string `code` when the error has its own (upper-snake, such as
  `SPENDING_LIMIT_EXCEEDED`), and otherwise the numeric HTTP status, so `code` no
  longer always equals the status.
  - `Error.Code` is the new `Code` type: the named code, or the numeric status in
    decimal (`"403"`), falling back to the HTTP status when the body has no
    usable code. `Code.Int()` and `Code.IsNumeric()` tell the two apart.
  - `Error.Details` is the envelope's allow-listed `details` as
    `map[string]any`, or nil.
  - `Error.StatusCode` stays the HTTP status, and `Kind` is still chosen from it:
    a 403 with `SPENDING_LIMIT_EXCEEDED` is `ErrAccessDenied`.
  - `Error()` includes a named code after the status:
    `topolab: access_denied (403 SPENDING_LIMIT_EXCEEDED): …`. Messages for
    errors without a named code are unchanged.
  - `docs/errors.md` lists the known string codes and what they mean.

### Added
- `Client.Datasets.Owned` and `Client.Datasets.IterOwned` — page the datasets the
  organization licences (`GET /v1/dataset/owned`). `IterOwned` is a Go 1.23
  range-over-func that pages by offset until the reported total is reached and
  stops on a short page. Entries carry absolute links, with `{format}`
  placeholders, and nullable `RecordCount` / `LatestArchiveMonth` /
  `Links.LatestArchive` as pointers.
- `Dataset.Archives` — list monthly archives, newest first (bare JSON array).
- `Dataset.Archive` — stream one monthly archive to disk, addressed by `latest`,
  `YYYY-MM` or `YYYY-MM-DD`. Months are validated as real calendar values
  client-side (leap years included), so an impossible month never costs a round
  trip or a credit.
- `Dataset.Coordinates` — coordinate rows with their attribute bag. The response
  body is a bare array and the paging facts arrive in the `X-Total-Count`,
  `X-Returned-Count` and `X-Offset` headers, which `CoordinatePage` carries;
  missing or unparseable headers fall back rather than erroring. `Latitude` and
  `Longitude` stay strings, as the API sends them.
- `Client.SQL` — read-only SQL across the datasets the organization licences
  (`POST /v1/sql/query`), requiring the Enterprise `sql-access` entitlement.
- `ErrQueryTimeout` / `KindQueryTimeout` for HTTP 408 (a query that outran the
  server statement timeout).
- Types: `OwnedDatasets`, `OwnedDataset`, `OwnedLinks`, `Archive`,
  `CoordinatePage`, `CoordinateRow`, `SQLResult`, `OwnedOptions`,
  `IterOwnedOptions`, `CoordinatesOptions`, `SQLOptions`.

### Fixed
- Add-on detection: the 403 message capture was `\w+`, which stops at a hyphen
  and so matched none of the hyphenated add-on slugs — every add-on 403 degraded
  to a plain access-denied error. Both message shapes now normalise to the slug
  (`api-access`, `archived-data`).
- Error envelope: the engine returns `{code, message, path, method, time,
  requestId}` and has no `statusCode` field. Mapping branches on the HTTP status,
  and `Error.RequestID` falls back to the body's `requestId` when the
  `X-Request-Id` header is absent.

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

[0.2.1]: https://github.com/topolab-bv/topolab-go/releases/tag/v0.2.1
[0.2.0]: https://github.com/topolab-bv/topolab-go/releases/tag/v0.2.0
[0.1.0]: https://github.com/topolab-bv/topolab-go/releases/tag/v0.1.0
