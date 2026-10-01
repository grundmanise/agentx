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

With [Homebrew](https://brew.sh), on macOS or Linux:

```sh
brew install grundmanise/tap/agentx
```

Or with the install script, which puts the latest release in `~/.local/bin`:

```sh
curl -fsSL https://agentx.wtf/install | sh
```

For the newest nightly build, end the command with `sh -s -- --nightly`. See
[Install and update](https://docs.agentx.wtf/install) for updates, options and uninstalling.

To build from source instead, use Go `1.27` or later:

```sh
git clone https://github.com/grundmanise/agentx.git
cd agentx/apps/cli && go build -o agentx .
```

On Linux, `CGO_ENABLED=0 go build` produces a static binary. On macOS, build with `cgo` enabled – the
default – so `agentx serve` watches through `FSEvents`; a macOS binary built without `cgo` falls back to
`kqueue`, which costs one file descriptor per watched file.

agentx runs your own `git`, 2.40 or later, found on your `PATH`.

## Documentation

Read the [documentation](https://docs.agentx.wtf) for every command, its flags and its output.

## Status

Supports 75 agents. 6 are read in full – skills, MCP servers and plugins:

**Claude Code** · **Codex** · **Cursor** · **Gemini CLI** · **Windsurf** · **GitHub Copilot**

Other agents support skills management only at this time.

> Under active development. On a single machine, the command-line tool already:
>
> - inventories every agent configuration and what it sees: [`agentx scan`](https://docs.agentx.wtf/cli/scan)
> - installs skills from git sources and places them in your agent clients: [`agentx skill add`](https://docs.agentx.wtf/cli/skill#install-a-skill)
> - checks for newer versions and applies them, merging your own edits into each update: [`agentx skill check`](https://docs.agentx.wtf/cli/skill#check-for-updates), [`agentx skill update`](https://docs.agentx.wtf/cli/skill#update-a-skill)
> - takes over the skills the vercel skills CLI installed, so agentx can update them too: [`agentx adopt`](https://docs.agentx.wtf/cli/adopt)
>
> Forking and editing skills, syncing across machines and the desktop app are in progress.

## Project docs

| | |
| --- | --- |
| [`CONTEXT.md`](CONTEXT.md) | The vocabulary |
| [`docs/adr`](docs/adr) | Architecture decisions |
| [`docs/spec`](docs/spec) | Contracts & Specs |
| [`docs/help-center`](docs/help-center) | Source of the [documentation](https://docs.agentx.wtf) |

## Contributing

Contributions are welcome. [`CONTRIBUTING.md`](CONTRIBUTING.md) covers setting up your machine, the
checks to run and how to open a pull request. Everyone taking part follows the
[Code of Conduct](CODE_OF_CONDUCT.md).

## License

[Apache License 2.0](LICENSE). Copyright 2026 Edgars Grundmanis.
