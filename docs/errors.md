# Errors

Every API failure is a single `*topolab.Error` type carrying a `Kind`. Branch on
the category with `errors.Is` against an exported sentinel, and read typed fields
with `errors.As`.

```go
import "errors"

_, err := ds.ToGeoJSON(ctx)
switch {
case errors.Is(err, topolab.ErrAddonRequired):
    var e *topolab.Error
    errors.As(err, &e)
    fmt.Println("needs add-on:", e.Addon)
case errors.Is(err, topolab.ErrRateLimit):
    // already retried; surface or back off further
case err != nil:
    return err
}
```

| Sentinel | `Kind` | When |
|---|---|---|
| `ErrAuthentication` | `authentication` | missing or invalid API key (401) |
| `ErrAddonRequired` | `addon_required` | key lacks the add-on — `.Addon` names it (403) |
| `ErrAccessDenied` | `access_denied` | dataset not accessible to your organization (403) |
| `ErrInsufficientCredit` | `insufficient_credits` | not enough credits — `.Required` / `.Available` (402) |
| `ErrNotFound` | `not_found` | unknown dataset or collection, or no archive available for that month (404) |
| `ErrQueryTimeout` | `query_timeout` | a SQL query outran the server statement timeout (408) |
| `ErrValidation` | `validation` | invalid request parameters (400/4xx) |
| `ErrRateLimit` | `rate_limit` | rate limited — `.RetryAfter`, retried automatically (429) |
| `ErrConfiguration` | `configuration` | client misconfiguration (missing key, invalid base URL) |
| `ErrServer` | `server` | upstream error (5xx), retried automatically |
| `ErrConnection` | `connection` | network failure or context cancellation |

## The error envelope

The engine answers with one envelope for every failure:

```json
{ "code": 403, "message": "This endpoint requires the api-access add-on",
  "path": "/v1/dataset/{table}/files/geojson", "method": "GET",
  "time": "2026-09-08T23:42:15.819Z", "requestId": "25c2a6a1…" }
```

There is **no `statusCode` field** — `code` mirrors the HTTP status — so the SDK
classifies purely on the **HTTP status**, never on a body field. `Error.RequestID`
is the `X-Request-Id` header, falling back to the body's `requestId`; quote it in
support requests.

## Add-on requirements

Add-on identifiers are hyphenated slugs (`api-access`, `gis-access`,
`archived-data`, `high-value-data`, `sql-access`). Two message shapes carry a
requirement, and both normalise to the same slug in `Error.Addon`:

| Message | `.Addon` |
|---|---|
| `This endpoint requires the api-access add-on` | `api-access` |
| `Archive access requires the Archived Data add-on. …` | `archived-data` |

A 403 that matches neither is an access denial (`ErrAccessDenied`) — an unknown
or unlicensed dataset on a data route.

## Retries

Transient statuses (`429`, `500`, `502`, `503`, `504`) and network errors are
retried with exponential backoff, honouring a `Retry-After` header when present.
`WithMaxRetries` (default 3) is the number of retries **after** the first attempt.
Context cancellation is never retried.
