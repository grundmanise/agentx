<div align="center">

# AgentX

**Control panel for your agent inventory**

`agentx` discovers skills, MCPs, and plugins from installed agents, allowing you to view, manage, distribute, and sync them across machines.

[![CI](https://img.shields.io/github/actions/workflow/status/grundmanise/agentx/ci.yml?branch=main&label=CI&style=flat-square)](https://github.com/grundmanise/agentx/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/grundmanise/agentx?filename=apps%2Fcli%2Fgo.mod&style=flat-square)](apps/cli/go.mod)
[![License](https://img.shields.io/github/license/grundmanise/agentx?style=flat-square)](LICENSE)
![Platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey?style=flat-square)
![Status](https://img.shields.io/badge/status-early%20development-orange?style=flat-square)

</div>

---

## Quick look

```console
$ agentx scan
3 configurations, 4 skills, 2 servers, 4 plugins detected

Claude Code (claude-code)  /home/me/.claude  enabled  3 skills, 1 plugin
  skills:
    commit  symlink    user  /home/me/.claude/skills/commit -> /home/me/.agents/skills/commit
    format  directory  user  /home/me/.claude/plugins/cache/acme-tools/formatter/1.2.0/skills/format  (plugin formatter)
    review  directory  user  /home/me/.claude/skills/review
  plugins:
    formatter  1.2.0  1 skill

Cursor (cursor)  /home/me/.cursor  enabled  2 skills, 2 servers, 1 plugin
  skills:
    commit  symlink    user  /home/me/.claude/skills/commit -> /home/me/.agents/skills/commit
    review  directory  user  /home/me/.claude/skills/review
  servers:
    context7  stdio            npx -y @upstash/context7-mcp
    stripe    streamable-http  https://mcp.stripe.com
```

## Install

No binary releases yet. Build from source with Go `1.27` or later:

```sh
git clone https://github.com/grundmanise/agentx.git
cd agentx/apps/cli && go build -o agentx .
```

On Linux, `CGO_ENABLED=0 go build` produces a static binary. On macOS, build with `cgo` enabled – the
default – so `agentx serve` watches through `FSEvents`; a macOS binary built without `cgo` falls back to
`kqueue`, which costs one file descriptor per watched file.

## Commands

| Command | What it does |
| --- | --- |
| [`agentx scan`](docs/help-center/cli/scan.mdx) | List every agent configuration on this machine and the skills, servers and plugins each one sees |
| [`agentx source`](docs/help-center/cli/source.mdx) | Add the git repositories you install skills from, list their skills, remove them |
| [`agentx serve`](docs/help-center/cli/serve.mdx) | Keep watching this machine and stream a snapshot whenever it changes |
| [`agentx doctor`](docs/help-center/cli/doctor.mdx) | Check that this machine can run agentx |
| [`agentx config`](docs/help-center/cli/config.mdx) | List, get and set this machine's settings |
| [`agentx machine`](docs/help-center/cli/machine.mdx) | Show this machine's id and label, rename it, reset the id |
| [`agentx version`](docs/help-center/cli/version.mdx) | Print the CLI version and its output schema version |

## Status

Supports 75 agents. 6 are read in full – skills, MCP servers and plugins:

**Claude Code** · **Codex** · **Cursor** · **Gemini CLI** · **Windsurf** · **GitHub Copilot**

Other agents support skills management only at this time.

> Under active development: The command-line tool inventories a single machine. Installing skills from sources, forking and editing them, and syncing across machines are all in progress, as is the desktop app.

## Documentation

| | |
| --- | --- |
| [`CONTEXT.md`](CONTEXT.md) | The vocabulary |
| [`docs/adr`](docs/adr) | Architecture decisions |
| [`docs/spec`](docs/spec) | Contracts & Specs |
| [`docs/help-center`](docs/help-center) | User documentation |

## Development

`make check` runs what CI runs on every pull request, in the same order, against the Go module in
`apps/cli`. A green `make check` means a green pull request.

| Target | Command |
| --- | --- |
| `fmt` | `gofmt -w .` |
| `fmt-check` | `gofmt -l .`, fails when any file is listed |
| `lint` | `golangci-lint run ./...` with `apps/cli/.golangci.yml`, including `go vet` |
| `tidy-check` | `go mod tidy`, fails when `go.mod` or `go.sum` change |
| `build` | `go build ./...`, `CGO_ENABLED=0` on Linux and `1` on macOS |
| `test` | `go test -race -count=1 ./...` |
| `check` | `fmt-check lint tidy-check build test` |

`golangci-lint` needs no install: its version is pinned in `apps/cli/.golangci-lint-version`, and
`make lint` builds that release into the build cache on first use through `go run`. CI installs the
same version.

Commands are [cobra](https://github.com/spf13/cobra) commands; the tree is built in
`apps/cli/internal/cli/run.go`, and every test drives `cli.Run` against a temporary home.

## License

[Apache License 2.0](LICENSE). Copyright 2026 Edgars Grundmanis.
