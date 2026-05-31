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
| `ErrNotFound` | `not_found` | unknown dataset or collection (404) |
| `ErrValidation` | `validation` | invalid request parameters (400/4xx) |
| `ErrRateLimit` | `rate_limit` | rate limited — `.RetryAfter`, retried automatically (429) |
| `ErrConfiguration` | `configuration` | client misconfiguration (missing key, invalid base URL) |
| `ErrServer` | `server` | upstream error (5xx), retried automatically |
| `ErrConnection` | `connection` | network failure or context cancellation |

## Retries

Transient statuses (`429`, `500`, `502`, `503`, `504`) and network errors are
retried with exponential backoff, honouring a `Retry-After` header when present.
`WithMaxRetries` (default 3) is the number of retries **after** the first attempt.
Context cancellation is never retried.
