# agentx

A desktop app and CLI that inventories the AI agent clients, skills, MCP servers and plugins on a developer's machines. `CONTEXT.md` defines the vocabulary; `docs/adr` holds the decisions, `docs/spec` the contracts, `docs/help-center` the user documentation.

## Checks

`make check` runs what the `CI` workflow (`.github/workflows/ci.yml`) runs on every pull request, in the same order, against the Go module in `apps/cli`. A green `make check` means a green pull request.

| Target | Command |
| --- | --- |
| `fmt` | `gofmt -w .` |
| `fmt-check` | `gofmt -l .`, fails when any file is listed |
| `lint` | `golangci-lint run ./...` with `apps/cli/.golangci.yml` (includes `go vet`) |
| `tidy-check` | `go mod tidy`, fails when `go.mod` or `go.sum` change |
| `build` | `go build ./...` with `CGO_ENABLED=0` on Linux and `1` on macOS, where the serve watcher uses FSEvents |
| `test` | `go test -race -count=1 ./...` |
| `check` | `fmt-check lint tidy-check build test` |

Requires Go 1.24 and, for `make lint`, the golangci-lint version pinned in the `Makefile`; `make lint` prints the install command when the tool is missing.
