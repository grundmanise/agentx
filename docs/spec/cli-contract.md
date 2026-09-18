# CLI contract

Status: accepted, 2026-09-18. Implements the output contract in ADR 0001 and the machine state layout in ADR 0004. The desktop app, scripts and tests depend on this document; change it together with the CLI.

## Invocation

`agentx [--json] [--verbose] <command> [arguments]`. The two global flags are accepted before or after the command. `--json` switches stdout to newline-delimited JSON events. `--verbose` raises the log level on stderr to debug. `agentx help`, `--help`, `-h` and a bare `agentx` print usage and exit 0; the usage text goes to stdout, or to stderr in JSON mode. A bare `agentx --json` is a usage error instead: a script that omits the command has made a mistake.

The CLI reads its environment once, at startup, from the variables below. Nothing else in the environment changes its behaviour.

| Variable | Meaning | Default |
|---|---|---|
| `AGENTX_HOME` | agentx home | `$HOME/.agentx` |
| `AGENTX_LIBRARY` | the library | `$HOME/.agents/skills` |
| `HOME` | the user's home; agent client paths derive from it | required |
| `XDG_CONFIG_HOME` | where agent clients keep user-scope configuration | `$HOME/.config` |
| `AGENTX_PLATFORM_ID` | the platform id the machine id is derived from; set it empty to declare that the machine has none | the operating system's platform id, see [Machine identity](#machine-identity) |
| `AGENTX_HOSTNAME` | the default machine label | the operating system's hostname |

## Streams

Without `--json`, stdout carries plain aligned text and stderr carries log lines as `level: message`.

With `--json`, stdout carries only events, one JSON object per line, and nothing else. Stderr carries log events in the same envelope. A consumer reads the exit code before trusting either stream.

## Event envelope

Every event is one JSON object with at least:

| Field | Type | Meaning |
|---|---|---|
| `type` | string | the event type |
| `schema_version` | integer | the output schema version, currently `1` |

`schema_version` changes when an event changes incompatibly. A consumer refuses a CLI whose schema version it does not know.

## Event types

Every run emits exactly one `result` as its last stdout event, after any `error`.

`version`, emitted by `agentx version`:

| Field | Type | Meaning |
|---|---|---|
| `cli_version` | string | the CLI version, `dev` for an unreleased build |

`settings`, emitted by every `agentx config` command:

| Field | Type | Meaning |
|---|---|---|
| `settings` | object | the machine settings as they now are, in the shape of the settings file below, with `label` filled in from the hostname when it is not set |

`machine`, emitted by every `agentx machine` command:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | the machine id, 32 lowercase hex characters |
| `label` | string | the machine label |
| `derivation` | string | `platform` when the id is derived from the platform id, `random` when it comes from `machine.json` |

`error`, emitted when a command fails:

| Field | Type | Meaning |
|---|---|---|
| `code` | string | the string code of the exit code table below |
| `message` | string | what went wrong, naming the path or command involved |
| `hint` | string | how to fix it; absent when there is nothing to suggest |

`result`, the terminating event:

| Field | Type | Meaning |
|---|---|---|
| `ok` | boolean | whether the command succeeded; the exit code is 0 exactly when it is `true` |
| `summary` | string | one line about the outcome; on failure the error message; absent when empty |

`log`, written to stderr:

| Field | Type | Meaning |
|---|---|---|
| `level` | string | the log level; `debug` lines appear only with `--verbose` |
| `message` | string | the log line |

The other event types are `progress`, `snapshot`, `reconcile`, `configuration`, `skill`, `mcp_server`, `plugin`, `doctor`, `search`, `refresh_complete`, `drift`, `update_available` and `conflict`. Their fields are added to this document by the command that first emits them. `operation` and `fleet` are reserved and never emitted.

## Exit codes

| Exit | Code | Meaning |
|---|---|---|
| 0 | `ok` | success |
| 1 | `usage` | a usage error: unknown command, bad flag, missing argument or a required environment variable not set |
| 2 | `git` | git is missing or older than 2.40 |
| 3 | `source` | a source is unreachable or authentication failed |
| 4 | `pending_merge` | a pending merge blocks the action |
| 5 | `not_found` | a skill, source or configuration was not found |
| 6 | `refused` | a collision or an unmet precondition |
| 7 | `locked` | another agentx command holds the lock |
| 8 | `account_repo` | the account repo is unusable: missing, corrupt, or a repository format this git cannot read |
| 10 | `internal` | an internal error, including an unreadable settings file |

In JSON mode the `error` event carries the code as its `code` field.

## Agentx home

Everything the CLI keeps on the machine lives under agentx home. Durable entries survive a reinstall when the directory is retained; transient entries coordinate running commands; derived entries are rebuilt from what the machine already has.

```
~/.agentx/
  account.git/        durable   bare account repo: skills/<name> forks, managed/<name> import branches,
                                refs/agentx/merge/<name> pending merges, refs/agentx/candidate/<name>
                                update candidates, refs/agentx/sources/<id> the last fetched state of each source
  worktrees/<name>/   durable   one linked worktree per placed fork
  settings.json       durable   machine settings
  mutations/<id>.json durable   local mutation journal, kept until completion or recovery
  sync.json           durable   fleet upload protocol metadata; reserved, nothing writes it today
  ops/<ulid>.json     durable   operation records; reserved, nothing writes them today
  version             transient mutation counter, rewritten last by every mutating command
  lock                transient advisory lock: shared for local scans, exclusive for mutations and recovery
  machine.json        derived   the machine id, only where no platform identity exists
  handshakes.json     derived   last MCP signature per server
```

No agent client reads anything in agentx home except the fork worktrees, through symlinks from the library. There is no logs directory: logs go to stderr. Temporary worktrees for editor sessions live in the operating system's temporary directory.

## Machine settings

`settings.json` in agentx home holds what only this machine decides. It is read whole and written by temp file, fsync and rename while the lock is held. A missing file means defaults. An unreadable file is exit code 10 with a hint naming the path.

`agentx config list` prints every setting, `agentx config get <key>` one, and `agentx config set <key> <value>` changes `label`, `auto_push` or `accept_operations`; the booleans take `true` or `false`. An unknown key is exit code 5 with a hint listing the keys; a key that `config set` cannot change, or an invalid value, is exit code 1. Without `--json`, `list` prints a `key  value` table and `get` prints the bare value.

```json
{
  "schema_version": 1,
  "label": "my-laptop",
  "auto_push": false,
  "accept_operations": false,
  "enabled_configurations": ["claude-code", "cursor"],
  "sources": [
    {
      "url": "https://github.com/example/skills",
      "alias": "https://github.com/example/skills-old",
      "pin": "v1.2.0",
      "last_fetched": "2026-09-18T10:00:00Z"
    }
  ],
  "copy_mode": {
    "my-skill": ["cursor"]
  }
}
```

| Key | Meaning |
|---|---|
| `schema_version` | the settings schema version, currently `1` |
| `label` | the machine label; absent until set with `config set label` or `machine rename`, and reported as the hostname meanwhile |
| `auto_push` | whether published forks are pushed automatically |
| `accept_operations` | reserved; whether this machine executes operations queued for it |
| `enabled_configurations` | configuration ids selected by default for explicit placements; every detected configuration is enabled on first detection |
| `sources` | each source by canonical `url`, with optional `alias` (a second URL mapped to the canonical one), optional `pin` (a ref the source is held at) and `last_fetched` (RFC 3339) |
| `copy_mode` | skill name to the configuration ids that receive a copy instead of a symlink |

Git config in the account repo holds only what git owns: remotes and tracking branches.

## Machine identity

The machine id names one computer across reinstalls. It is derived, in this order:

1. When `machine.json` exists in agentx home, its `id` is the machine id and the derivation is `random`. The file is `{"id": "<32 lowercase hex characters>"}`.
2. Otherwise, when a platform id exists, the machine id is HMAC-SHA256 with the key `agentx-machine-id/v1` over the message `<platform id> LF <uid>`, where `LF` is one newline byte and `<uid>` is the numeric user id in decimal, rendered as the first 32 lowercase hex characters of the MAC. The derivation is `platform`. The platform id is `AGENTX_PLATFORM_ID` when the variable is set, else the content of `/etc/machine-id` without surrounding whitespace on Linux, else `IOPlatformUUID` from `ioreg -rd1 -c IOPlatformExpertDevice` on macOS.
3. Otherwise 16 random bytes are generated once, stored as hex in `machine.json`, and used from then on as in 1.

`agentx machine` prints the id, the label and the derivation. `agentx machine rename <label>` sets the label in the settings file. `agentx machine reset-id` writes a new random id to `machine.json`; from then on the id is random even where a platform id exists, until the file is deleted.

## Lock and version file

Every command that changes agentx home takes an exclusive advisory `flock` on `lock` in agentx home without waiting, does its writes, rewrites `version` as its last step and releases the lock. `version` holds one decimal integer and a newline, incremented on every successful mutation (a missing file counts as 0); it is a change signal for watchers, not an ordering of snapshots. A command that finds the lock held exits at once with code 7 and a hint naming the lock file. A failed mutation leaves `version` untouched. Reading commands do not take the lock today. The first mutation creates agentx home and its `ops` directory; nothing writes into `ops` yet.

## Content hash

The content hash identifies one version of a skill by its content. It is SHA-256 over the following byte sequence, where `NUL` is one zero byte:

1. the frontmatter `name`, then `NUL`;
2. the frontmatter `description`, then `NUL`;
3. for every regular file under the skill directory, in bytewise order of its relative path with `/` as the separator: the relative path, `NUL`, the byte length in decimal, `NUL`, the file bytes.

A missing name or description contributes empty bytes. A symlink inside the skill is followed when it resolves inside the skill directory and skipped with a warning otherwise. File modes and times do not contribute. The hash is rendered as a short type prefix and lowercase hex; the prefix is fixed by the command that first emits it.
