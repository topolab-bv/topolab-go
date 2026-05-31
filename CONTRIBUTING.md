# Contributing

Thanks for your interest in the Topolab Go SDK.

## Development

```bash
go test ./...        # run the suite
go test -race ./...  # with the race detector (covers concurrent ItemsAll)
go vet ./...         # static checks
gofmt -l .           # formatting (must be empty)
```

The package has **no third-party dependencies** — keep it that way; the standard
library is enough for an HTTP + JSON client.

## Conventions

The public surface is shared across all Topolab SDKs and lint-checked against
[`topolab-sdk-spec`](https://github.com/topolab-bv/topolab-sdk-spec). If you add
or rename a method, update the spec in the same change.

Run `go test`, `go vet`, and `gofmt` before opening a pull request. Issues and
PRs are welcome.
