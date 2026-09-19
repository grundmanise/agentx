# agentx

A desktop app and CLI that inventories the AI agent clients, skills, MCP servers and plugins on a developer's machines. `CONTEXT.md` defines the vocabulary; `docs/adr` holds the decisions, `docs/spec` the contracts, `docs/help-center` the user documentation.

## CLI

```sh
cd apps/cli && go build -o agentx .   # the binary
```

Requires Go 1.27. Commands are [cobra](https://github.com/spf13/cobra) commands; the tree is built in `apps/cli/internal/cli/run.go` and every test drives `cli.Run` against a temporary home. On Linux `CGO_ENABLED=0 go build` produces a static binary. On macOS build with cgo enabled, the default there, so `agentx serve` watches through FSEvents; a macOS binary built without cgo falls back to kqueue, which costs one descriptor per watched file. The output contract is in `docs/spec/cli-contract.md`.

## Checks

`make check` runs what the `CI` workflow (`.github/workflows/ci.yml`) runs on every pull request, in the same order, against the Go module in `apps/cli`. A green `make check` means a green pull request.

| Target | Command |
| --- | --- |
| `fmt` | `gofmt -w .` |
| `fmt-check` | `gofmt -l .`, fails when any file is listed |
| `lint` | `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v<pin> run ./...` with `apps/cli/.golangci.yml` (includes `go vet`) |
| `tidy-check` | `go mod tidy`, fails when `go.mod` or `go.sum` change |
| `build` | `go build ./...` with `CGO_ENABLED=0` on Linux and `1` on macOS, where the serve watcher uses FSEvents |
| `test` | `go test -race -count=1 ./...` |
| `check` | `fmt-check lint tidy-check build test` |

Requires Go 1.27. golangci-lint needs no install: its version is pinned in `apps/cli/.golangci-lint-version`, `make lint` builds that release into the build cache on first use through `go run`, and CI installs the same version. To bump it, edit the file.
