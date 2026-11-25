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

https://github.com/user-attachments/assets/ef627466-8ad5-44c3-b3b6-f105fd109c36

## Install

### The app (recommended)

On a Mac with Apple silicon, with [Homebrew](https://brew.sh). This also puts the `agentx` CLI on your `PATH`:

```sh
brew install grundmanise/tap/agentx
```

On Linux, download the `.deb` or the AppImage from the [latest release](https://github.com/grundmanise/agentx/releases/latest). Both carry the CLI; the `.deb` also puts
`agentx` on your `PATH`.

### Alternatively, CLI only

For a machine with no desktop, on macOS or Linux, with Homebrew:

```sh
brew install grundmanise/tap/agentx-cli
```

Or, on Linux or a Mac with Apple silicon, with the install script, which installs only the CLI, to `~/.local/bin`:

```sh
curl -fsSL https://agentx.wtf/install | sh
```

For the newest nightly build, end the install script command with `sh -s -- --nightly`. To update a CLI
that the install script installed, run `agentx update`. See [Install and update](https://docs.agentx.wtf/install)
for options, updates and uninstalling.

agentx needs `git` 2.40 or later on your `PATH`.

### Build from source

To build the CLI, use Go `1.27.1` or later:

```sh
git clone https://github.com/grundmanise/agentx.git
cd agentx/apps/cli && go build -o agentx .
```

On Linux, `CGO_ENABLED=0 go build` produces a static binary. On macOS, build with `cgo` enabled – the
default – so `agentx serve` watches through `FSEvents`; a macOS binary built without `cgo` falls back to
`kqueue`, which costs one file descriptor per watched file.

To build the app, see [`apps/desktop/README.md`](apps/desktop/README.md).

## Documentation

[docs.agentx.wtf](https://docs.agentx.wtf)

## Status

Supports 73 agents. The 10 most used agents get full support first.

| Agent | Skills | MCP servers | Plugins |
| --- | :---: | :---: | :---: |
| Claude Code, Codex, Cursor, Gemini CLI | ✓ | ✓ | ✓ |
| GitHub Copilot | ✓ | ✓ | planned |
| Devin | ✓ | ✓ | – |
| Antigravity, OpenCode | ✓ | planned | planned |
| Cline, Junie | ✓ | planned | – |
| All others | ✓ | | |

– means the agent has no such feature.

## Goals and non-goals

Goal: one person sees, manages and improves the skills, MCP servers and memories of all their agents, on all their machines and in the cloud.

V1, global only ([#401](https://github.com/grundmanise/agentx/issues/401)):

- Manage skills across agents and machines
- Create custom skills
- Show MCP servers and plugins, read-only
- Cloud agents: Claude Code, Codex and Cursor

V2 ([#402](https://github.com/grundmanise/agentx/issues/402)):

- Project scope for all of the above, with no automatic repo scanning
- Manage MCP servers and plugins
- Manage memories
- Stats for skills, tools and MCP servers, from your own machines

Later:

- Unified memory across agents and machines
- Agents manage their own skills and MCP servers
- Learn which skills bring value
- Recommended skills and MCP servers

Not goals:

- Distributing skills to a team or a company
- A hosted marketplace or registry
- Running agents, or acting as an agent runtime
- Windows

See [ADR 0020](docs/adr/0020-agentx-is-for-one-person-and-their-one-hub.md).

## Project docs

| | |
| --- | --- |
| [`GLOSSARY.md`](GLOSSARY.md) | The vocabulary |
| [`docs/adr`](docs/adr) | Architecture decisions |
| [`docs/spec`](docs/spec) | Contracts & Specs |
| [`docs/help-center`](docs/help-center) | Source of the [documentation](https://docs.agentx.wtf) |

## Contributing

Contributions are welcome. [`CONTRIBUTING.md`](CONTRIBUTING.md) covers setting up your machine, the
checks to run and how to open a pull request. Everyone taking part follows the
[Code of Conduct](CODE_OF_CONDUCT.md).

## License

[Apache License 2.0](LICENSE). Copyright 2026 Edgars Grundmanis.
