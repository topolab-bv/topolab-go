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
| `ErrAccessDenied` | `access_denied` | dataset not accessible to your organization, or the monthly spending limit is reached (`.Code == "SPENDING_LIMIT_EXCEEDED"`) (403) |
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

```json
{ "code": "SPENDING_LIMIT_EXCEEDED",
  "message": "Organization monthly spending limit exceeded",
  "details": { "limit": 5000, "consumed": 4995, "remaining": 5,
               "periodEnd": "2026-11-30T23:59:59.999Z" },
  "path": "/v1/dataset/{table}/files/geojson", "method": "GET",
  "time": "2026-11-07T11:52:28.000Z", "requestId": "8f0c41d2…" }
```

There is **no `statusCode` field**. `code` is a **string** when the error has its
own code (upper-snake, such as `SPENDING_LIMIT_EXCEEDED`) and otherwise the
**numeric HTTP status**, so it does not always equal the status. The SDK
classifies purely on the **HTTP status**, never on a body field, and `Code`
refines the meaning inside a `Kind`: a 403 with `SPENDING_LIMIT_EXCEEDED` is still
`ErrAccessDenied`.

Every `*topolab.Error` built from a response carries:

| Field | Type | Meaning |
|---|---|---|
| `StatusCode` | `int` | HTTP status of the response. Use this for numeric checks. |
| `Code` | `topolab.Code` | The envelope's `code`: a named code, or the numeric status in decimal (`"403"`). The HTTP status when the body has no usable code (for example a proxy's error page). `Code.Int()` and `Code.IsNumeric()` tell the two apart. |
| `Details` | `map[string]any` | The envelope's allow-listed `details`, or `nil`. JSON numbers decode as `float64`. |
| `RequestID` | `string` | The `X-Request-Id` header, falling back to the body's `requestId`; quote it in support requests. |

Errors raised before a response exists (`ErrConnection`, or `ErrConfiguration`
from the client itself) have a zero `StatusCode`, an empty `Code` and nil
`Details`. For a named code, `Error()` includes it after the status:
`topolab: access_denied (403 SPENDING_LIMIT_EXCEEDED): …`.

```go
err := ds.Download(ctx, "poi.geojson", "geojson")
var e *topolab.Error
if errors.As(err, &e) && e.Code == "SPENDING_LIMIT_EXCEEDED" {
    fmt.Println("monthly limit reached; resets at", e.Details["periodEnd"])
}
```

Compare named codes with `==`. If you used to read the body's `code` as the HTTP
status, read `StatusCode` instead.

## Known string codes

Codes this SDK's calls can return:

| Status | `Code` | Sentinel | Meaning | `Details` |
|---|---|---|---|---|
| 403 | `SPENDING_LIMIT_EXCEEDED` | `ErrAccessDenied` | A metered call (download, SQL) would pass the organization's monthly spending limit. | `limit`, `consumed`, `remaining` (numbers), `periodEnd` (ISO 8601 timestamp) |
| 400 | `SQL_DATA_ERROR` | `ErrValidation` | The SQL query raised a data error while evaluating a value, such as a bad cast. The message names the SQLSTATE. | none |
| 400 | `MAX_NODES_EXCEEDED`, `MAX_DEPTH_EXCEEDED`, `MAX_ARRAY_LENGTH_EXCEEDED`, `MAX_KEYS_EXCEEDED` | `ErrValidation` | A JSON request body is larger or deeper than the server accepts. | none |
| 400 | `PROTOTYPE_POLLUTION_KEY` | `ErrValidation` | A JSON request body contains a forbidden key (`__proto__`, `constructor`, `prototype`). | none |

Numeric codes that carry `Details`:

| `Code` | Sentinel | `Details` |
|---|---|---|
| `"402"` | `ErrInsufficientCredit` (also on `.Required` / `.Available`) | `required`, `available` |
| `"413"` | `ErrValidation` | `quotaBytes`, `usageBytes`, `additionalBytes` (Data Studio uploads) |

Other parts of the API use further codes, listed in the
[SDK conventions](https://github.com/topolab-bv/topolab-sdk-spec/blob/main/conventions.md#known-string-codes):
`HIGH_VALUE_DATA_REQUIRED` (403, `details.columns` / `details.datasetIds`),
`REGENERATE_REQUIRED` (410) and `METADATA_NOT_READABLE` (400) from Data Studio,
`SSO_REQUIRED` (403) and `SSO_CHECK_UNAVAILABLE` (503) from sign-in, and the
order add-on and delivery-destination codes. The chat API reports a failed turn
on its event stream with lower-snake codes such as `ai_busy`, `ai_interrupted`
and `ai_cancelled`; this SDK does not call it.

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
