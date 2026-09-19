# CLI contract

Status: accepted, 2026-09-18. Implements the output contract in ADR 0001 and the machine state layout in ADR 0004. The desktop app, scripts and tests depend on this document; change it together with the CLI.

## Invocation

`agentx [--json] [--verbose] <command> [arguments]`. The two global flags are accepted before or after the command. `--json` switches stdout to newline-delimited JSON events. `--verbose` raises the log level on stderr to debug. `agentx help`, `--help`, `-h` and a bare `agentx` print usage and exit 0; the usage text goes to stdout, or to stderr in JSON mode. A bare `agentx --json` is a usage error instead: a script that omits the command has made a mistake.

The CLI reads its environment once, at startup, from the variables below. Beyond them, `PATH` locates git and the command of a local MCP server, and the [handshake](#handshake) hands the environment on to the servers it starts.

| Variable | Meaning | Default |
|---|---|---|
| `AGENTX_HOME` | agentx home | `$HOME/.agentx` |
| `AGENTX_LIBRARY` | the library | `$HOME/.agents/skills` |
| `HOME` | the user's home; agent client paths derive from it | required |
| `XDG_CONFIG_HOME` | where agent clients keep user-scope configuration | `$HOME/.config` |
| `AGENTX_PLATFORM_ID` | the platform id the machine id is derived from; set it empty to declare that the machine has none | the operating system's platform id, see [Machine identity](#machine-identity) |
| `AGENTX_HOSTNAME` | the default machine label | the operating system's hostname |
| `AGENTX_INSTANCE_ID` | the `instance_id` carried by snapshots, so a script can fix it | a fresh random id per process |
| `AGENTX_HANDSHAKE_TIMEOUT` | the budget of one MCP handshake, a duration such as `500ms` | `10s` |

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

`snapshot`, emitted by `agentx scan`, is described under [Snapshot](#snapshot).

`configuration`, `skill`, `mcp_server` and `plugin` are node types carried inside `snapshot`; no command emits them as events of their own. `doctor` is described under [Doctor](#doctor) and `refresh_complete` under [Serve](#serve). The types `progress`, `reconcile`, `search`, `drift`, `update_available` and `conflict` are named for later commands and not emitted yet; `operation` and `fleet` are reserved and never emitted.

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
  serve.lock          transient advisory lock held by the one serve child of this home for its lifetime
  machine.json        derived   the machine id, only where no platform identity exists
  handshakes.json     derived   last MCP signature per server
```

No agent client reads anything in agentx home except the fork worktrees, through symlinks from the library. There is no logs directory: logs go to stderr. Temporary worktrees for editor sessions live in the operating system's temporary directory.

## Machine settings

`settings.json` in agentx home holds what only this machine decides. It is read whole and written by temp file, fsync and rename while the lock is held. A missing file means defaults. An unreadable file is exit code 10 with a hint naming the path.

`agentx config list` prints every setting, `agentx config get <key>` one, and `agentx config set <key> <value>` changes `label`, `auto_push` or `accept_operations`; the booleans take `true` or `false`. An unknown key, a key that `config set` cannot change, or an invalid value is exit code 1 with a hint listing the keys. Without `--json`, `list` prints a `key  value` table and `get` prints the bare value.

`agentx config disable <id>` adds a configuration id to `disabled_configurations` and `agentx config enable <id>` removes it; both are mutations, both are no-ops when the list already has the wanted state, and both emit the `settings` event. The id must name a detected configuration (see [Snapshot](#snapshot)), else exit code 5 with a hint listing the detected ids. A detected configuration is enabled unless it is listed, so every configuration is enabled on first detection without a write.

```json
{
  "schema_version": 1,
  "label": "my-laptop",
  "auto_push": false,
  "accept_operations": false,
  "disabled_configurations": ["windsurf"],
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
| `disabled_configurations` | configuration ids the user took out of the default set for explicit placements, sorted; every other detected configuration is enabled |
| `sources` | each source by canonical `url`, with optional `alias` (a second URL mapped to the canonical one), optional `pin` (a ref the source is held at) and `last_fetched` (RFC 3339) |
| `copy_mode` | skill directory name to the configuration ids that receive a copy instead of a symlink |

Git config in the account repo holds only what git owns: remotes and tracking branches.

## Machine identity

The machine id names one computer across reinstalls. It is derived, in this order:

1. When `machine.json` exists in agentx home, its `id` is the machine id and the derivation is `random`. The file is `{"id": "<32 lowercase hex characters>"}`.
2. Otherwise, when a platform id exists, the machine id is HMAC-SHA256 with the key `agentx-machine-id/v1` over the message `<platform id> LF <uid>`, where `LF` is one newline byte and `<uid>` is the numeric user id in decimal, rendered as the first 32 lowercase hex characters of the MAC. The derivation is `platform`. The platform id is `AGENTX_PLATFORM_ID` when the variable is set, else the content of `/etc/machine-id` without surrounding whitespace on Linux, else `IOPlatformUUID` from `ioreg -rd1 -c IOPlatformExpertDevice` on macOS.
3. Otherwise 16 random bytes are generated once, under the lock, stored as hex in `machine.json`, and used from then on as in 1.

`agentx machine` prints the id, the label and the derivation. `agentx machine rename <label>` sets the label in the settings file. `agentx machine reset-id` writes a new random id to `machine.json`; from then on the id is random even where a platform id exists, until the file is deleted.

## Lock and version file

Every command that changes agentx home takes an exclusive advisory `flock` on `lock` in agentx home without waiting, does its writes, rewrites `version` as its last step and releases the lock. `version` holds one decimal integer and a newline, incremented on every successful mutation (a missing file counts as 0); it is a change signal for watchers, not an ordering of snapshots. A command that finds the lock held exits with code 7 and a hint naming the lock file after at most 50 ms of retrying; it never waits for the holder. While it holds the exclusive lock, a command keeps its process id in the lock file, so `agentx doctor` can name the holder. A failed mutation leaves `version` untouched. `agentx scan` holds a shared `flock` on the same file while it reads settings and the filesystem, retrying for up to one second while a mutation holds the exclusive lock and exiting with code 7 after that; it never writes `version`. With `--handshake` it releases the shared lock before it starts or connects to any server, and writes `handshakes.json` afterwards under the exclusive lock, again without touching `version`. Other reading commands do not take the lock, except for the one write that stores a random machine id; that write does not touch `version`. Taking either lock creates agentx home and its `ops` directory when they are missing; nothing writes into `ops` yet.

## Content hash

The content hash identifies one version of a skill by its content. It is SHA-256 over the following byte sequence, where `NUL` is one zero byte:

1. the frontmatter `name`, then `NUL`;
2. the frontmatter `description`, then `NUL`;
3. for every regular file under the skill directory, in bytewise order of its relative path with `/` as the separator: the relative path, `NUL`, the byte length in decimal, `NUL`, the file bytes.

A missing name or description contributes empty bytes; a missing or unparsable frontmatter counts as both missing, even though the skill is then named after its directory. A symlink inside the skill is followed when it resolves inside the skill directory and skipped with a warning otherwise: a file symlink contributes the target's bytes under the symlink's path, a directory symlink is walked under the symlink's path, and a directory symlink that points back at a directory being walked is a loop, skipped with a warning. File modes and times do not contribute. The hash is rendered as bare lowercase hex in the `content_hash` field of a skill node.

## Snapshot

`agentx scan` inventories the machine and emits one `snapshot` event: every detected agent configuration, every skill each one can see, every MCP server each one declares and every plugin each one has installed, with the skills and servers a plugin provides. It writes nothing and never starts or connects to a server unless `--handshake` is given. Two scans of an unchanged machine produce byte-identical events when `instance_id` is fixed.

`agentx scan --project <path>` adds the project-scope skills found under `<path>` in each detected configuration's project skills directories, read-only; `<path>` must exist, else exit code 5. `agentx scan --configuration <id>` names the configuration that changed; the id must be detected, else exit code 5 with a hint listing the detected ids, and the whole machine is scanned regardless, since a one-shot command has no earlier snapshot to reuse.

Without `--json`, the output is one section per configuration, headed `<name> (<id>)  <path>  enabled|disabled`, with one line per occurrence: skill name, kind, scope and placement path, followed by ` -> <resolved path>` for a symlink and by `  (plugin <name>)` for a placement inside a plugin. A configuration that declares servers continues with a `servers:` line and one line per server: name, transport, then the command line or the URL, with `  (plugin <name>)` for a server a plugin provides, then `<n> tools` (`1 tool`) for a server with a signature, counting its tools, prompts, resources and resource templates together, ending with `  (disabled)` for a server its client records as turned off. A configuration with plugins continues with a `plugins:` line and one `name  version` line per plugin, ending with `  (disabled)` for a plugin its client records as disabled. Environment and header values are never printed. Warnings go to stderr as `warning: <message>`.

The event:

| Field | Type | Meaning |
|---|---|---|
| `instance_id` | string | fresh per process, or `AGENTX_INSTANCE_ID` |
| `scan_counter` | integer | `1` for a one-shot scan |
| `machine` | object | `id`, the machine id, and `label` |
| `configurations` | array | the detected configurations, sorted by `id` |
| `skills` | array | the skill nodes, sorted by `physical_id` |
| `mcp_servers` | array | the MCP server nodes, sorted by `physical_id` |
| `plugins` | array | the plugin nodes, sorted by `physical_id` |
| `edges` | array | `{"from", "to"}` pairs of node ids, sorted by `from` then `to`: the machine id to each configuration; each configuration to each skill it sees, each server it declares and each plugin it has installed; each plugin to each skill and server it provides |
| `warnings` | array of strings | what could not be read, sorted; a warning never fails the scan |

A configuration:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | the configuration id: the client's slug, such as `claude-code`, `cursor`, `codex`, `gemini-cli`, `windsurf`, `github-copilot` |
| `client` | string | the client's slug |
| `name` | string | the client's display name |
| `path` | string | the user-scope configuration directory; the configuration is detected because it exists |
| `enabled` | boolean | `false` when the id is in `disabled_configurations` |
| `reads_library` | boolean | whether the client reads the library directly, so a library skill needs no placement in its own directory |
| `physical_id`, `logical_id` | string | the same value, see below |

A skill node is one content version seen on this machine; the same content found through several placements is one node with several occurrences:

| Field | Type | Meaning |
|---|---|---|
| `physical_id`, `logical_id` | string | see below |
| `name` | string | the frontmatter `name`, or the directory name when the frontmatter is missing, unparsable or has no name |
| `description` | string | the frontmatter `description`, or empty |
| `content_hash` | string | the content hash, lowercase hex |
| `occurrences` | array | sorted by `id` |

An occurrence:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | see below |
| `configuration` | string | the configuration id |
| `path` | string | the placement path as the client sees it |
| `resolved_path` | string | the real directory after following every symlink |
| `kind` | string | `symlink` when the placement path is a symlink; `copy` when it is a real directory in user scope and `copy_mode` lists the configuration under the directory's name; `directory` for every other real directory |
| `scope` | string | `user`, or `project` under `--project` |
| `plugin` | string | the name of the plugin the placement is inside; absent for every other placement |

An MCP server node is one server in one signature version; the same server declared in several configurations is one node with several occurrences:

| Field | Type | Meaning |
|---|---|---|
| `physical_id`, `logical_id` | string | see below |
| `name` | string | the key the server is declared under, from its first declaration in scan order |
| `signature` | string | the signature hash, see the handshake below: what the server exposed to this scan's handshake, else the last one stored in `handshakes.json`; `none`, the fixed no-signature marker, when neither exists |
| `signature_at` | string | when the signature was taken, RFC 3339 in UTC; absent with `none` |
| `tools` | array | what the server exposed, one `{"id", "kind", "name", "description"}` per tool, prompt, resource or resource template, `kind` being `tool`, `prompt`, `resource` or `resource_template`, sorted by `kind` then `name`; absent with `none` or when the server exposed nothing. No edge points at a tool: it is reached through its server |
| `occurrences` | array | sorted by `id` |

A server occurrence:

| Field | Type | Meaning |
|---|---|---|
| `id` | string | see below |
| `configuration` | string | the configuration id |
| `config_file` | string | the file the server is declared in |
| `command` | string | the command of a local server, or empty |
| `args` | array of strings | its arguments, in order |
| `env_keys` | array of strings | the names of the environment variables the declaration sets, sorted; the values are never recorded |
| `url` | string | the URL of a remote server as written, or empty |
| `header_keys` | array of strings | the names of the headers the declaration sets, sorted; the values are never recorded |
| `transport` | string | `stdio` when the declaration has a command; `sse` when its `type` or `transport` says so, or, for Gemini CLI, when it uses `url` rather than `httpUrl`; `streamable-http` for every other URL |
| `handshake` | boolean | whether this scan handshook the declaration: `true` only under `--handshake` and only when the handshake succeeded |
| `plugin` | string | the name of the plugin that declares the server; absent for a configuration's own servers |
| `enabled` | boolean | `false` when the client records this declaration as turned off, which today is a Codex plugin server whose `config.toml` table carries `[plugins."<name>@<marketplace>".mcp_servers.<server>]` with `enabled = false`; absent from every other occurrence, including one whose overlay sets `enabled = true` or is missing. The server node is unchanged: a turned-off declaration is still declared and still merges by identity |

A plugin node is one plugin bundle installed in one configuration, in one version:

| Field | Type | Meaning |
|---|---|---|
| `physical_id`, `logical_id` | string | see below |
| `name` | string | the plugin name |
| `marketplace` | string | where the plugin was installed from: the marketplace name for Claude Code and Codex, the marketplace directory name for a cached Cursor plugin (`cursor-public` is the official marketplace), the recorded install source for a Gemini CLI extension; empty for a Cursor plugin under `plugins/local` and whenever the client records none |
| `version` | string | the installed version, or empty when the client records none |
| `configuration` | string | the configuration id |
| `path` | string | the plugin's directory |
| `enabled` | boolean | whether the client has the plugin enabled; present only when the client records that state on disk, which today is Codex for a plugin with a `config.toml` table; absent for every other plugin, which is installed but not known to be enabled or disabled |

Server discovery reads these user-scope files, each parsed as a map of server name to declaration under its top-level key, ignoring unknown keys; a declaration that is not an object or has neither a command nor a URL is ignored; a missing file declares nothing; a file that cannot be read or parsed is one warning naming the file, never its content, and the scan continues:

| Configuration | File | Format |
|---|---|---|
| `claude-code` | `~/.claude.json` | JSON, `mcpServers` |
| `codex` | `~/.codex/config.toml` | TOML, `[mcp_servers.<name>]` tables with `command`, `args`, `env`, `url`, `http_headers` and `env_http_headers` |
| `cursor` | `~/.cursor/mcp.json` | JSON, `mcpServers` |
| `gemini-cli` | `~/.gemini/settings.json` | JSON, `mcpServers`, with `url` an SSE endpoint and `httpUrl` a streamable HTTP one |
| `windsurf` | `~/.codeium/windsurf/mcp_config.json` | JSON, `mcpServers`, with `url` or `serverUrl` |
| `github-copilot` | `~/.copilot/mcp-config.json` | JSON, `mcpServers` |

In the JSON files a declaration holds `command`, `args` and `env` for a local server, `url` (or the client's variant) and `headers` for a remote one, and an optional `type` or `transport`. Every other configuration contributes skills only.

### Handshake

`agentx scan --handshake` starts or connects to every declared server after the local reads, outside the lock, at most four at a time, and lists what each exposes. Each server has a budget of ten seconds; `AGENTX_HANDSHAKE_TIMEOUT`, a duration such as `500ms`, replaces it for tests and diagnosis. A server on the `stdio` transport is started as its command and arguments, with the environment of the `agentx` process plus the variables the declaration sets, and its standard error goes to the null device; when the handshake is over its standard input is closed, a server still running a second later receives `SIGTERM`, and one still running a second after that is killed. A server on `streamable-http` receives JSON-RPC by `POST` to its URL with the declared headers (for Codex, an `env_http_headers` value is read from the environment of the `agentx` process), `Accept: application/json, text/event-stream`, and answers either as JSON or as an event stream; the `Mcp-Session-Id` the server assigns is echoed and released with `DELETE` at the end. A remote server is reached through the proxy the environment of the `agentx` process names (`HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY`), the one place a handshake reads that environment directly. The exchange is `initialize` with protocol version `2025-11-25`, the last MCP revision that opens with an `initialize` handshake (the server's answer is accepted whatever version it names; a server that implements only the `2026-07-28` revision or later, which carries the version on every request instead, rejects `initialize` and is one warning), `notifications/initialized`, then `tools/list`, `prompts/list`, `resources/list` and `resources/templates/list`, each only when the initialize result declares its capability, following `nextCursor` until the last page. A method the server reports as unknown (JSON-RPC error `-32601`) contributes nothing. The legacy `sse` transport is not handshaken and is one warning.

A server that cannot be started, exits, answers something that is not JSON-RPC, refuses a method with any other error or runs out of its budget is one warning, `<config file>: server <name>: <reason>`, that never carries an environment or header value; the scan continues and the server keeps its stored signature, or `none`. The exit code stays 0.

Every item is serialised as its kind, name, description, input schema and output schema joined by `NUL`, the schemas as canonical JSON (object keys sorted, no whitespace, numbers as written, only the escapes JSON requires) and empty for anything but a tool or an absent schema. Items are sorted by kind, then name, then serialisation. The signature is SHA-256 over the serialisations joined by `NUL`, rendered as 64 lowercase hex characters; a server that exposes nothing has the signature of the empty list. A tool's hash is SHA-256 over its own serialisation, and its `id` is the identity `tool` over the server's logical id and that hash, so a tool that is byte-identical in two versions of one server is one node.

After the handshakes, the signatures taken are written to `handshakes.json` in agentx home under the exclusive lock, replacing the entry of each server handshaken and keeping every other: `{"<server logical id>": {"signature": "<hex>", "tools": [...], "at": "<RFC 3339>"}}`, `tools` as on the node. The file is derived state, so `version` is not rewritten: no other consumer needs to rescan for it. When one scan handshakes two declarations of one logical server that answer differently, each declaration gets its own node, and the file keeps the declaration last in scan order. A scan without `--handshake` reads the file and gives every declaration of a listed server the stored `signature`, `signature_at` and `tools`; a file that cannot be parsed is a warning and counts as empty.

Plugin discovery reads, per configuration:

| Configuration | Install records | Plugin directories | Manifest |
|---|---|---|---|
| `claude-code` | `~/.claude/plugins/installed_plugins.json`, keyed `<name>@<marketplace>`, each record with `installPath` and `version` | the `installPath` | `.claude-plugin/plugin.json`, for the version when the record has none |
| `codex` | the `[plugins."<name>@<marketplace>"]` tables of `~/.codex/config.toml`, each with `enabled` (`true` when absent) | `~/.codex/plugins/cache/<marketplace>/<name>/<version>/`; the active version is `local` when present, else the highest by semver, compared as strings where either name is not a semver | `plugin.json` at the root when its `$schema` starts with `https://agent-plugins.org/schemas/`, else the first of `.codex-plugin/plugin.json`, `.claude-plugin/plugin.json` and `.cursor-plugin/plugin.json` |
| `cursor` | none on disk: the account holds them | `~/.cursor/plugins/local/<dir>` and `~/.cursor/plugins/cache/<marketplace>/<name>/<version>/` holding a `.cache-complete` file | the first of `.cursor-plugin/plugin.json`, `.claude-plugin/plugin.json` and `plugin.json` |
| `gemini-cli` | none: every directory under `~/.gemini/extensions` holding a `gemini-extension.json` is an extension | that directory | `gemini-extension.json`, which also declares the `mcpServers`; the marketplace is the `source` in `.gemini-extension-install.json` next to it, when present |

Every other configuration reports no plugins. For every client, a manifest or server file that cannot be parsed is one warning naming the file, never its content, and the plugin is still reported with what its directory gives; a missing file is silent. The skills a plugin provides are discovered in its skills directories exactly like a skills directory (direct children only), merged with every other skill by content hash and recorded as occurrences carrying the plugin's name; its servers are recorded as occurrences carrying the plugin's name with the file they were read from as `config_file`.

Claude Code: a record without an `installPath` or whose directory does not exist is a warning; skills are under `skills` and servers in `.mcp.json` at the root, in the `mcpServers` JSON format.

Codex: the name and marketplace are the two halves of the `config.toml` key, split on its last `@`; a key without both halves is a warning, and a key whose plugin directory does not exist is a warning naming that directory. A plugin directory without a key, which is how a plugin installed through the account appears, is reported without an `enabled` field. The version is the manifest's `version`, else the version directory name. Skills are the manifest's `skills` paths (one string or a list, each starting with `./`), else `skills`. Servers are the manifest's `mcpServers` object, recorded with the manifest as `config_file`, or the file that field names, else `.mcp.json`, else `mcp.json`; the file holds the Codex `config.toml` fields (`command`, `args`, `env`, `url`, `http_headers`, `env_http_headers`) as JSON, either under `mcpServers` or as a bare map of name to declaration. A `[plugins."<name>@<marketplace>".mcp_servers.<server>]` table with `enabled = false` marks that server's occurrence `enabled: false`; its other keys are ignored.

Cursor: a plugin under `plugins/local` has an empty `marketplace`; a hidden entry or a file there is skipped, and a symlink is followed only when it resolves inside `plugins/local`, else it is a warning naming the link. A cached plugin takes its `marketplace` and `name` from the directory names, the manifest's `name` replacing the latter when present, and its `version` from the manifest, else the version directory name (a commit id or `release_<tag>`); an entry without `.cache-complete` is a warning naming the directory and reports no plugin, and every complete version is reported. A plugin without a manifest is named after its directory. Cursor records neither what is installed nor what is enabled on disk, so a reported plugin is installed as far as the cache shows, not necessarily enabled, and its node has no `enabled` field. Skills are in the manifest's `skills` paths (one or a list, each starting with `./` and staying inside the plugin, else a warning naming the path), else under `skills`; servers are the manifest's `mcpServers` object, with the manifest as the `config_file`, else the file the manifest names, else `mcp.json`, else `.mcp.json`, in the `mcpServers` JSON format, with `${CURSOR_PLUGIN_ROOT}` and `${CLAUDE_PLUGIN_ROOT}` replaced by the plugin directory in the command, the arguments and the environment values. The Claude Code plugins Cursor imports from `~/.claude/plugins` are reported under `claude-code` only, never a second time under `cursor`.

Discovery: inside each skills directory a client reads, every child directory, or symlink to one, that holds a `SKILL.md` is a skill; hidden entries and `node_modules` are skipped; a broken symlink is a warning. A client's user-scope skills directories are its own, other clients' directories it reads (Cursor reads the Claude Code and Codex directories) and the library for clients that read it directly (Codex and Gemini CLI), which yields an occurrence with the library path as placement path whatever the enabled state. Each directory is canonicalised once and each skill is hashed once per scan.

Identities are SHA-256 over the type name and its inputs, each input preceded by one `NUL` byte, rendered as `<type>:<lowercase hex>`. Paths under the user's home are hashed with the home replaced by `~`, so an identity does not depend on where the home directory is. Every identity but a skill's, a plugin's and a remote or package server's logical one includes the machine id; since the platform-derived id also covers the user id, a snapshot that must compare across users or machines, such as the CLI's own golden files, pins the id with `machine.json`.

| Identity | Inputs |
|---|---|
| machine node id | the machine id itself, no hash |
| configuration | `configuration`, machine id, configuration id, configuration path |
| skill, logical | `skill`, `content`, content hash: an unmanaged skill is its content |
| skill, physical | `skill`, logical id, machine id, content hash |
| skill occurrence | `occurrence`, configuration physical id, skill physical id, scope, placement path |
| server, logical, remote | `server`, `url`, the normalised URL: lowercase scheme and host, no user information, no default port (80 for `http`, 443 for `https`), no trailing slash, no query or fragment; a remote server is one whose URL has a host other than `localhost` or an IP address |
| server, logical, package | `server`, the registry, the package: `npm` and the first non-flag argument of `npx` without its `@version`; `pypi` and the first non-flag argument of `uvx` or of `pipx run` before any version specifier or extras; `docker` and the first argument of `docker run` that is neither a flag nor the value of one (`-e`, `-v`, `-p`, `--name`, `-w`, `--network`, `--entrypoint`, `-u`, `--mount`, `--env-file`, `--platform`, `-l`, `--add-host`, `-h` and their long forms), without its tag or digest |
| server, logical, other | `server`, `machine`, machine id, command, URL, each argument, with the home replaced by `~` in the command and the arguments: a server that is neither remote nor a package never matches one on another machine |
| server, physical | `server`, logical id, machine id, signature |
| tool | `tool`, server logical id, tool hash |
| server occurrence | `occurrence`, configuration physical id, server physical id, config file, server name |
| plugin, logical | `plugin`, marketplace, name |
| plugin, physical | `plugin`, logical id, machine id, configuration physical id, version |

## Git

The CLI runs the system git as a subprocess and never embeds a git implementation. Git is located through `PATH` of the environment the CLI was given. The floor is git 2.40, compared by numeric version components, so 2.4 and 2.39 are rejected and 2.100 is accepted. Before any command other than `version`, `doctor` and `help` runs, the CLI runs `git --version` once; a missing or too-old git is exit code 2 with a hint naming the distribution package where known, before the command does anything. `version` and `doctor` stay available to report the problem.

Every git call names its repository with `--git-dir` explicitly, captures stdout and stderr, and logs the command line and stderr at debug level. Git runs in one of two environments, built from the CLI's own environment:

- Isolated, for every command that writes objects or merges: `GIT_CONFIG_GLOBAL` points at `/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, every `GIT_*` variable of the user's is dropped, author and committer are fixed to `agentx <agentx@localhost>` at `946684800 +0000`, and `core.autocrlf=false`, `commit.gpgsign=false` and `core.hooksPath=/dev/null` are passed with `-c`. A commit made this way has the same id on every machine whatever the user's git configuration.
- User, for network commands: the CLI's environment as is, so credential helpers, SSH configuration and URL rewrites apply. agentx never stores git credentials.

In the serve child every git call additionally has `GIT_TERMINAL_PROMPT=0`, `-o BatchMode=yes` appended to the SSH command and `GIT_ASKPASS=/bin/false`, so a prompt becomes an error rather than a hang.

## Doctor

`agentx doctor` checks whether this machine can run agentx and reports one row per check, in this order:

| Check | Statuses | What it means |
|---|---|---|
| `git` | `ok`, `fail` | git is in `PATH` and 2.40 or newer; the detail names the version |
| `merge_tree` | `ok`, `fail` | `git merge-tree --write-tree --merge-base=<base> <ours> <theirs>` merges two branches of a throwaway repository in the temporary directory, made with deterministic commits |
| `relative_worktree_paths` | `info` | whether git is 2.48 or newer, which enables relative worktree paths |
| `isolated_commit` | `ok`, `fail` | the isolated environment gives a fixed input the known commit id `5d75017e77f5413f4337ef776244b8d8dc77ca90` |
| `home` | `ok`, `fail` | agentx home exists, or was created, and is writable |
| `lock` | `ok`, `warn` | the lock is free, or held by another agentx command; the detail names the holder's process id |
| `settings` | `ok`, `fail` | `settings.json` parses, or does not exist yet; the detail and hint name the path |
| `account_repo` | `ok`, `fail` | the account repo opens, or was created by this run |
| `library` | `ok`, `warn` | the library directory exists; a missing library is a warning, not a failure |

`doctor`, one event per check:

| Field | Type | Meaning |
|---|---|---|
| `check` | string | the check name from the table above |
| `status` | string | `ok`, `warn`, `fail` or `info` |
| `detail` | string | what was found, naming the version, path or error involved |
| `hint` | string | how to fix it; absent when there is nothing to suggest |

Without `--json` the same rows print as a `check  status  detail  hint` table. Exit code 2 when git is missing or too old; the run stops after the `git` row, since nothing else can be checked. Exit code 8 when the account repo is unusable, after every row. Exit code 7 when the account repo has to be created and another command holds the lock. Otherwise 0, warnings included. The `result` event carries `ok: false` exactly when the exit code is non-zero.

## Account repo

The account repo is `account.git` in agentx home, a bare repository. It is created on first use, under the lock, by the first command that opens it; `agentx doctor` is that command on a fresh machine. Creation runs `git init --bare` in the isolated environment and sets `gc.auto=0` (maintenance runs on the serve child's timer, never inside a command), `core.logAllRefUpdates=true` (reflogs, which a bare repository lacks by default), `merge.conflictStyle=zdiff3` and, on git 2.48 or newer, `worktree.useRelativePaths=true`. The repository is renamed into place only once every step succeeded. A present `account.git` that is not a bare repository git can read is exit code 8 with a hint naming the path.

## Serve

`agentx serve --json` is the long-running child the desktop app holds open. It scans once on start, emits that snapshot, then rescans when the machine changes and emits the snapshot again only when the inventory changed. It runs until its stdin closes or its context is cancelled, then emits `result` and exits 0. Without `--json` it prints one line per event: `snapshot <counter>: <n> configurations, <n> skills` and `refresh <request_id>: ok, snapshot <counter>`.

One serve child per agentx home: on start it takes an exclusive advisory `flock` on `serve.lock` in agentx home and holds it until it exits. A second `agentx serve` for the same home, `--once` included, exits at once with code 6 and a hint naming `serve.lock`. This lock is distinct from `lock`: serve never blocks a mutating command.

Snapshots: every serve process has a fresh `instance_id` (or `AGENTX_INSTANCE_ID`). The initial scan always emits a `snapshot` with `scan_counter` `1`, even when an earlier process saw identical content. Each later scan is compared with the last emitted snapshot on its serialised bytes with `instance_id` and `scan_counter` excluded; an identical snapshot emits nothing, a different one increments the counter and is emitted whole. The desktop applies snapshots only from its active child and only with increasing counters, as [snapshot ordering](snapshot-ordering.md) prescribes.

Scans: every scan holds the shared `flock` on `lock` over its local reads, like `agentx scan`, but waits for a mutation in progress for as long as it takes instead of giving up after a second; it never takes the exclusive lock. Scans are serialised, one at a time. A change signal that arrives during a scan schedules one more scan after it.

Change signals: serve watches, in this order, agentx home (where every mutating command rewrites `version` last), `account.git`, `worktrees` and the library, one non-recursive watch per directory, on the real path after resolving symlinks. A directory that does not exist at start is skipped and tried again after every scan. Events are debounced: a rescan runs 100 ms after the last event, or 500 ms after the first event of a burst, whichever comes first. When the watcher cannot be created or a watch fails, serve emits one `log` event with level `warn` on stderr saying why and rescans every 2 s instead.

Stdin carries requests, one JSON object per line. The one request is `{"type": "refresh", "request_id": "<unique id>"}`. A refresh is satisfied only by a scan that begins after the request was received; a scan already in progress cannot satisfy it, and every request received before the next scan begins shares that scan. After the scan, a changed snapshot is emitted first, then one `refresh_complete` per request. A line that is not a JSON object, a request without a known `type`, or a refresh without a `request_id` is answered with an `error` event with code `usage` on stdout, and serve keeps running; under serve an `error` event therefore does not announce the end of the run. Blank lines are ignored.

`refresh_complete`:

| Field | Type | Meaning |
|---|---|---|
| `request_id` | string | the id from the request |
| `instance_id` | string | this serve process's instance id |
| `ok` | boolean | whether the scan succeeded |
| `scan_counter` | integer | the counter of the current snapshot, changed or not; absent when `ok` is `false`, since a failed scan claims no freshness |
| `error` | string | why the scan failed; present only when `ok` is `false` |

A failed rescan is also reported as a `log` warning; the last snapshot stays in force and serve keeps running. A failed initial scan ends serve with the error's exit code.

`agentx serve --once` takes the serve lock, runs the initial scan, emits its snapshot and exits 0 with exactly the events `snapshot` and `result`. It watches nothing and reads no requests.
