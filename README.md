# agentx

A desktop app and CLI that inventories the AI agent clients, skills, MCP servers and plugins on a developer's machines. `CONTEXT.md` defines the vocabulary; `docs/adr` holds the decisions, `docs/spec` the contracts, `docs/help-center` the user documentation.

## CLI

```sh
cd apps/cli && go build ./... && go test ./...
go build -o agentx .   # the binary
```

Requires Go 1.24. Commands are [cobra](https://github.com/spf13/cobra) commands; the tree is built in `apps/cli/internal/cli/run.go` and every test drives `cli.Run` against a temporary home. On Linux `CGO_ENABLED=0 go build` produces a static binary. On macOS build with cgo enabled, the default there, so `agentx serve` watches through FSEvents; a macOS binary built without cgo falls back to kqueue, which costs one descriptor per watched file. The output contract is in `docs/spec/cli-contract.md`.
