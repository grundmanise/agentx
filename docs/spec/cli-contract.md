# CLI contract

Status: accepted, 2026-09-18. Implements the output contract in ADR 0001 and the machine state layout in ADR 0004. The desktop app, scripts and tests depend on this document; change it together with the CLI.

## Invocation

`agentx [--json] [--verbose] [--color on|off] <command> [arguments]`. The three global flags are accepted before or after the command. `--json` switches stdout to newline-delimited JSON events. `--verbose` raises the log level on stderr to debug. `--color` decides whether text output carries ANSI colour, see [Streams](#streams); any other value is a usage error. `agentx help`, `--help`, `-h` and a bare `agentx` print usage and exit 0; the usage text goes to stdout, or to stderr in JSON mode. A bare `agentx --json` is a usage error instead: a script that omits the command has made a mistake.

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

Without `--json`, stdout carries aligned text and stderr carries log lines as `level: message`, with `error: <message>` and `hint: <fix>` for a failure. A table pads every column to its widest cell with two spaces between columns and nothing after the last cell. A row that reports a state starts with a glyph: `✓` ok, `!` warning, `✗` failure, `•` information. A command that changes something confirms it on one line starting with `✓`; the wording is the command's own.

Text output is coloured per stream when that stream is a terminal, the `NO_COLOR` variable is not set (see no-color.org) and `TERM` is not `dumb`; `--color on` colours a pipe too, and `--color off` colours nothing. Colour is added by SGR escape sequences around the text and changes nothing else: stripping the sequences gives the exact text a pipe receives, alignment included. JSON output is never coloured, whatever `--color` says. The palette has one meaning per colour: bold for a heading or the name that starts a row, cyan for the key of a key-value row and for a command or flag name, dim for secondary detail, green for ok and enabled, yellow for a warning, a hint and disabled, bold red for a failure and an error, blue for information, magenta for a `(plugin <name>)` marker, cyan for a count such as `12 tools`.

Text agentx did not write — a skill's name and description, which the repository a source was added from supplies — is sanitised before it is printed: every control character becomes a space, runs of spaces become one and the leading and trailing ones are dropped. A value carrying a newline therefore cannot break the one row per item a listing prints, a tab cannot disturb a column, a carriage return cannot overwrite the line already printed, and no escape sequence reaches the terminal, whatever `--color` says. Nothing is parsed: agentx does not recognise an escape sequence and strip it whole, since a parser that misjudged one sequence's end would let the rest through, while a rule that admits no control character at all cannot; what a sequence leaves behind once its `ESC` is a space, the `[31m` of a red, prints as the ordinary text it is. Sanitising happens before anything is painted, so the rule above still holds: stripping the SGR sequences agentx adds gives the exact text a pipe receives. JSON output is not sanitised, its values being escaped already, so a consumer reads what the source wrote.

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

`source`, emitted by every `agentx source` command, one per source (see [Sources](#sources)):

| Field | Type | Meaning |
|---|---|---|
| `id` | string | the source id, 16 lowercase hex characters |
| `url` | string | the canonical URL |
| `alias` | string | a second URL mapped onto the canonical one; absent until aliases exist |
| `pin` | string | the ref the source is pinned to; absent when it follows the remote's default branch |
| `subpath` | string | the directory the command's listing was scoped to; absent for the whole repository, never stored |
| `last_fetched` | string | when the source was last fetched, RFC 3339 in UTC; absent from the event of `source remove` |
| `commit` | string | the fetched commit; absent when the account repo holds no ref for the source |
| `previous_commit` | string | what the source ref held before this fetch; absent when the fetch did not move it and when the source is new |
| `skills` | integer | how many skills the listing found; present after `add`, `fetch` and `skills`, absent from `list` and `remove` |

`source_skill`, emitted by `agentx source skills`, one per installable skill, after the `source` event, sorted by `subpath`:

| Field | Type | Meaning |
|---|---|---|
| `source` | string | the source id |
| `subpath` | string | the skill directory from the repository root, `""` when the root itself is the skill |
| `name` | string | the frontmatter `name`, else the directory name (the repository name for the root) |
| `description` | string | the frontmatter `description`, `""` when it has none |
| `tree` | string | the tree id of the skill directory at the fetched commit |

`progress`, emitted by a command that does one thing several times, one event per step as that step ends:

| Field | Type | Meaning |
|---|---|---|
| `phase` | string | what the step did, one word per kind of step the command has: `fetch` for one source of `agentx source fetch` |
| `subject` | string | what the step was about: the source's canonical URL for a fetch; absent for a step that covers the run as a whole |
| `current` | integer | how many steps have ended, this one included, so that one run counts from 1 to `total` |
| `total` | integer | how many steps the run has in all; absent when the run does not know that in advance |

A step is reported when it ends rather than when it starts, so that `current` is the count a progress bar needs and a run emits one event per step however it schedules them. Steps taken in parallel end in the order the network decides, so the order of the subjects is not fixed and is not the order the run reports its results in; each event says on its own what it covers, which is what a consumer reads. The event carries no outcome: a step that failed is a `log` warning, and the `error` and `result` events name what failed.

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

`configuration`, `skill`, `mcp_server` and `plugin` are node types carried inside `snapshot`; no command emits them as events of their own (`source_skill` is not the `skill` node). `doctor` is described under [Doctor](#doctor), and `refresh_complete` and `search` under [Serve](#serve). The types `reconcile`, `drift`, `update_available` and `conflict` are named for later commands and not emitted yet; `operation` and `fleet` are reserved and never emitted.

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
                                update candidates, refs/agentx/sources/<id> the last fetched state of each
                                source, refs/agentx/fetching/<id> where a fetch of that source stages until
                                it is whole (see Sources)
  worktrees/<name>/   durable   one linked worktree per placed fork
  settings.json       durable   machine settings
  mutations/<id>.json durable   local mutation journal, kept until completion or recovery; the content it
                                stages waits in mutations/<id>.staged next to it (see Mutation journal)
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

`agentx config list` prints every setting, `agentx config get <key>` one, and `agentx config set <key> <value>` changes `label`, `auto_push` or `accept_operations`; the booleans take `true` or `false`. An unknown key, a key that `config set` cannot change, or an invalid value is exit code 1 with a hint listing the keys. Without `--json`, `list` prints a `key  value` table, showing an empty value as `(none)`, `get` prints the bare value, and `set`, `enable` and `disable` print one confirmation line: `✓ <key> is now <value>`, `✓ <id> is now enabled` or `✓ <id> is now disabled`.

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
| `sources` | each source by canonical `url`, sorted by it, with optional `alias` (a second URL mapped to the canonical one; reserved, nothing sets it), optional `pin` (the ref the source is held at, absent when it follows the remote's default branch) and `last_fetched` (RFC 3339 in UTC); written by `source add`, `source remove` and `source fetch`, which rewrites `last_fetched` alone, see [Sources](#sources) |
| `copy_mode` | skill directory name to the configuration ids that receive a copy instead of a symlink |

Git config in the account repo holds only what git owns: remotes and tracking branches.

## Machine identity

The machine id names one computer across reinstalls. It is derived, in this order:

1. When `machine.json` exists in agentx home, its `id` is the machine id and the derivation is `random`. The file is `{"id": "<32 lowercase hex characters>"}`.
2. Otherwise, when a platform id exists, the machine id is HMAC-SHA256 with the key `agentx-machine-id/v1` over the message `<platform id> LF <uid>`, where `LF` is one newline byte and `<uid>` is the numeric user id in decimal, rendered as the first 32 lowercase hex characters of the MAC. The derivation is `platform`. The platform id is `AGENTX_PLATFORM_ID` when the variable is set, else the content of `/etc/machine-id` without surrounding whitespace on Linux, else `IOPlatformUUID` from `ioreg -rd1 -c IOPlatformExpertDevice` on macOS.
3. Otherwise 16 random bytes are generated once, under the lock, stored as hex in `machine.json`, and used from then on as in 1.

`agentx machine` prints the id, the label and the derivation. `agentx machine rename <label>` sets the label in the settings file. `agentx machine reset-id` writes a new random id to `machine.json`; from then on the id is random even where a platform id exists, until the file is deleted. Without `--json`, `machine` prints a `key  value` table with the rows `Machine id`, `Label` and `Derivation`, and the two mutations print one confirmation line: `✓ machine label is now <label>` or `✓ machine id is now <id>`.

## Lock and version file

Every command that changes agentx home takes an exclusive advisory `flock` on `lock` in agentx home without waiting, recovers any unfinished [mutation journal](#mutation-journal), does its writes through a journal of its own, rewrites `version` as its last step and releases the lock. `version` holds one decimal integer and a newline, incremented on every successful mutation (a missing file counts as 0); it is a change signal for watchers, not an ordering of snapshots. A command that finds the lock held exits with code 7 and a hint naming the lock file after at most 50 ms of retrying; it never waits for the holder. The one exception is a **take-back**: a command undoing, under a later hold, a write an earlier hold of its own already made. Giving up there would leave that write behind for good, since the lock is exactly what the command needs to clean up after itself, so a take-back waits for the holder for up to five seconds before it gives up — long enough for a queue of ordinary mutations, each of which holds the lock for one file write, and short enough that a lost lock never becomes a hang. A take-back leaves `version` untouched, like any step whose change nothing watching agentx home ever saw. While it holds the exclusive lock, a command keeps its process id in the lock file, so `agentx doctor` can name the holder. A failed mutation leaves `version` untouched. `agentx scan` holds a shared `flock` on the same file while it reads settings and the filesystem, retrying for up to one second while a mutation holds the exclusive lock and exiting with code 7 after that; it never writes `version`. With `--handshake` it releases the shared lock before it starts or connects to any server, and writes `handshakes.json` afterwards under the exclusive lock, again without touching `version`. Other reading commands do not take the lock, except for the one write that stores a random machine id; that write does not touch `version`. Taking either lock creates agentx home and its `mutations` and `ops` directories when they are missing; nothing writes into `ops` yet.

## Mutation journal

Every replacement of a state file in agentx home is journaled, so that a process stopped at any point leaves either the old file or the new one and a later command can tell which and finish the job. This covers `settings.json` (`config set`, `config enable`, `config disable`, `machine rename`, `source add`, `source fetch`, `source remove`), `machine.json` (`machine reset-id`, and the first command that stores a random machine id) and `handshakes.json` (`scan --handshake`). `version` is a signal, not state, and is rewritten atomically without a journal. Creating the account repo is not journaled: it builds the repository in a temporary directory and renames it into place.

Under the exclusive lock the command writes the new content to `mutations/<id>.staged`, then the journal `mutations/<id>.json`, each by temp file, fsync, rename and directory fsync. Only then does it rename the staged file over the live file, rewrite the journal with `progress` `applied` and remove it. `<id>` is the Unix time in nanoseconds, `-` and four random bytes in hex, so journals sort oldest first. The journal is one JSON object:

| Field | Meaning |
|---|---|
| `progress` | `staged` until every live file is replaced, then `applied` |
| `replace` | the live files the mutation replaces, one entry each |
| `replace[].path` | the live file's absolute path |
| `replace[].old` | SHA-256 hex of the live file before the mutation, or `absent` when there was no file |
| `replace[].new` | SHA-256 hex of the staged content |
| `replace[].staged` | the staged file, next to the journal |

Recovery runs under the exclusive lock and decides from what the live file holds, not from `progress`, since a process can stop after the rename and before recording it. A live file whose hash is `new` is done: the journal, and a staged file still present, are removed and `version` is not bumped. A live file whose hash is `old` while the staged file hashes to `new` gets the staged file renamed over it, which bumps `version` once. A live file that matches neither was changed meanwhile: nothing is touched, the file, the staged content and the journal all stay, and the command fails with exit code 6 (`refused`) and a message that says `recovery required` and names the file and the journal, with a hint to restore the file's previous content and rerun, or to move the journal aside to keep the file as it is. A `.json` file under `mutations/` that is not a journal, or one whose staged file is missing or does not hash to `new`, is refused the same way. Recovery never repeats a completed step, so it can run any number of times. Journals are recovered oldest first and recovery stops at the first refusal. A `.staged` file without a journal, left by a process that stopped between the two writes, is removed once every journal is recovered.

A process that stops after the rename and before it rewrites `version`, like a recovery that finds the live file already new, leaves the new file in place without a `version` bump. That is a missed signal, not a lost change: `agentx serve` still rescans, since the file lives in agentx home, which it watches, and every other command reads the file fresh.

Every mutating command recovers before its own change, under the lock it already holds; a refused recovery refuses the command. `agentx scan` and every scan of `agentx serve` look for journals under the shared lock and, when one exists, release it, recover under the exclusive lock (waiting for a mutation in progress the way they wait for the shared lock), release that and start their reads over, so no inventory is composed from a half-applied change. A refused recovery is exit code 6 for `agentx scan` and for the initial scan of `agentx serve`; for a later scan of `agentx serve` it is a `log` warning on stderr and a `refresh_complete` with `ok: false` and the message in `error` for every pending refresh, while the last snapshot stays in force until the journal is resolved. `agentx doctor` reports unfinished journals in its `mutations` row and recovers nothing. Reading commands such as `config get` and `machine` neither check for journals nor recover.

## Content hash

The content hash identifies one version of a skill by its content. It is SHA-256 over the following byte sequence, where `NUL` is one zero byte:

1. the frontmatter `name`, then `NUL`;
2. the frontmatter `description`, then `NUL`;
3. for every regular file under the skill directory, in bytewise order of its relative path with `/` as the separator: the relative path, `NUL`, the byte length in decimal, `NUL`, the file bytes.

The frontmatter is the `---` block at the top of `SKILL.md`, read as YAML: `name` and `description` are taken from it in any scalar form the standard allows, a plain value, a quoted one, a literal or folded block, or a plain value continued on the indented lines below its key, with surrounding whitespace trimmed; a number or a boolean written without quotes contributes the text the file holds rather than the decoded value rendered back, so `007`, `1.50` and `True` hash as they are written and not as `7`, `1.5` and `true`; a key whose value is a mapping or a sequence contributes nothing, so a nested block such as `metadata` is skipped; and a block that is not valid YAML, an unclosed one and a duplicate key included, is unparsable. A `SKILL.md` comes from any repository a user adds, so a block longer than 65536 bytes, or one opening more than 256 lists or mappings, is refused unread: the parser builds its whole tree before it can report anything, and a block that nests far enough costs memory faster than bytes, which ends the process rather than raising an error. Published blocks run to a few hundred bytes. A missing name or description contributes empty bytes; a missing or unparsable frontmatter counts as both missing, even though the skill is then named after its directory. A symlink inside the skill is followed when it resolves inside the skill directory and skipped with a warning otherwise: a file symlink contributes the target's bytes under the symlink's path, a directory symlink is walked under the symlink's path, and a directory symlink that points back at a directory being walked is a loop, skipped with a warning. File modes and times do not contribute. The hash is rendered as bare lowercase hex in the `content_hash` field of a skill node.

## Snapshot

`agentx scan` inventories the machine and emits one `snapshot` event: every detected agent configuration, every skill each one can see, every MCP server each one declares and every plugin each one has installed, with the skills and servers a plugin provides. It writes nothing and never starts or connects to a server unless `--handshake` is given. Two scans of an unchanged machine produce byte-identical events when `instance_id` is fixed.

`agentx scan --project <path>` adds the project-scope skills found under `<path>` in each detected configuration's project skills directories, read-only; `<path>` must exist, else exit code 5. `agentx scan --configuration <id>` names the configuration that changed; the id must be detected, else exit code 5 with a hint listing the detected ids, and the whole machine is scanned regardless, since a one-shot command has no earlier snapshot to reuse.

Without `--json`, the output starts with one summary line counting the machine, `<n> configurations, <n> skills, <n> servers, <n> plugins detected` (`1 skill`), counting skills, servers and plugins as the snapshot does, once each however many occurrences; then one section per configuration, each preceded by a blank line and headed `<name> (<id>)  <path>  enabled|disabled` and, when the configuration has any, `  <n> skills, <n> servers, <n> plugins` (`1 skill`, leaving out a count of zero, counting the skill and server occurrences of this configuration), followed by up to three labelled blocks, each present only when it has rows: `skills:` with one line per occurrence (skill name, kind, scope and placement path, followed by ` -> <resolved path>` for a symlink and by `  (plugin <name>)` for a placement inside a plugin); `servers:` with one line per server (name, transport, then the command line or the URL, with `  (plugin <name>)` for a server a plugin provides, then `<n> tools` (`1 tool`) for a server with a signature, counting its tools, prompts, resources and resource templates together, ending with `(disabled)` for a server its client records as turned off); and `plugins:` with one `name  version` line per plugin, followed by what the plugin provides in this configuration as `<n> skills, <n> servers` (`1 skill`, leaving out a count of zero, nothing when both are zero), ending with `(disabled)` for a plugin its client records as disabled. The block labels are indented two spaces and their rows four; rows are sorted by name. A configuration with no rows at all says `no skills, servers or plugins`. With no configuration detected the output is `No agent configurations detected.` and a hint line instead. Environment and header values are never printed. Warnings go to stderr as `warning: <message>`.

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
| `handshake_error` | object | `{"reason", "message", "hint"}` when this scan's handshake of the declaration failed, see the handshake below; absent from every other occurrence |
| `plugin` | string | the name of the plugin that declares the server; absent for a configuration's own servers |
| `enabled` | boolean | `false` when the client records this declaration as turned off: a Codex declaration with `enabled = false`, a Codex plugin server turned off by its overlay, every server of a Codex plugin with `enabled = false` (see below), a Gemini CLI server listed in `mcp.excluded` of `~/.gemini/settings.json`, left out of a non-empty `mcp.allowed` there, or recorded as `{"enabled": false}` under its lowercased name in `~/.gemini/mcp-server-enablement.json`, and a Copilot CLI server listed in `disabledMcpServers` of `~/.copilot/settings.json`. Claude Code, Cursor and Windsurf keep that state per project or inside the app, and it is not read; absent from every other occurrence, including one the client records as on. A turned-off declaration is still declared and still merges into its server node by identity, and is never handshaken |

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
| `codex` | `~/.codex/config.toml` | TOML, `[mcp_servers.<name>]` tables with `command`, `args`, `env`, `cwd`, `url`, `http_headers`, `env_http_headers`, `bearer_token_env_var` and `enabled` |
| `cursor` | `~/.cursor/mcp.json` | JSON, `mcpServers` |
| `gemini-cli` | `~/.gemini/settings.json` | JSON, `mcpServers`, with `url` an SSE endpoint and `httpUrl` a streamable HTTP one; `mcp.excluded` and `mcp.allowed` turn servers off |
| `windsurf` | `~/.codeium/windsurf/mcp_config.json` | JSON, `mcpServers`, with `url` or `serverUrl` |
| `github-copilot` | `~/.copilot/mcp-config.json` | JSON, `mcpServers`; `disabledMcpServers` in `~/.copilot/settings.json` turns servers off |

In the JSON files a declaration holds `command`, `args`, `env` and an optional `cwd` for a local server, `url` (or the client's variant) and `headers` for a remote one, and an optional `type` or `transport`. Every other configuration contributes skills only.

### Handshake

`agentx scan --handshake` starts or connects to every declared server after the local reads, outside the lock, at most four at a time, and lists what each exposes. Each server has a budget of ten seconds; `AGENTX_HANDSHAKE_TIMEOUT`, a duration such as `500ms`, replaces it for tests and diagnosis. A declaration its client has turned off (`enabled: false` on its occurrence) is never started or connected to, and keeps its stored signature. A server on the `stdio` transport is started as its command and arguments in its declared `cwd`, a relative one resolved against the plugin's directory for a plugin's server and otherwise against the directory `agentx` runs in, as each client resolves it against its own; without one, it starts in the directory `agentx` runs in, or, for a server of a Codex plugin in the Agent Plugins format, in the plugin's directory; a command with a `/` resolves against that working directory, a bare one is looked up on the `PATH` of the `agentx` process. It runs with the environment of the `agentx` process plus the variables the declaration sets, and its standard error goes to the null device; when the handshake is over its standard input is closed, a server still running a second later receives `SIGTERM`, and one still running a second after that is killed. A remote server receives the declared headers; for Codex, an `env_http_headers` value is read from the environment of the `agentx` process, and a `bearer_token_env_var` that environment sets becomes `Authorization: Bearer <value>` (listed in `header_keys`; unset, no `Authorization` is sent). A server on `streamable-http` receives JSON-RPC by `POST` to its URL, `Accept: application/json, text/event-stream`, and answers either as JSON or as an event stream; the `Mcp-Session-Id` the server assigns is echoed and released with `DELETE` at the end. A remote server is reached through the proxy the environment of the `agentx` process names (`HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY`), the one place a handshake reads that environment directly. The exchange is `initialize` with protocol version `2025-11-25`, the last MCP revision that opens with an `initialize` handshake (the server's answer is accepted whatever version it names; a server that implements only the `2026-07-28` revision or later, which carries the version on every request instead, rejects `initialize` and is one warning), `notifications/initialized`, then `tools/list`, `prompts/list`, `resources/list` and `resources/templates/list`, each only when the initialize result declares its capability, following `nextCursor` until the last page. A method the server reports as unknown (JSON-RPC error `-32601`) contributes nothing.

Before a declaration is handshaken, the placeholders in its command, arguments, environment values, `cwd`, URL and header values are expanded from the environment of the `agentx` process the way its client expands them. The expansion serves the handshake alone: the snapshot records every value as written, so a server's identity never depends on the environment, and no expanded value is printed or stored. A default applies only when the variable is unset; a variable set to the empty string gives the empty string.

| Configuration | Placeholders | An unset variable |
|---|---|---|
| `claude-code` | `${VAR}`, `${VAR:-default}` | stays as written. In a remote server's URL and headers, the variables Claude Code treats as credentials are empty whether set or not, a default notwithstanding: the list of Claude Code 2.1.278 (such as `AWS_SECRET_ACCESS_KEY`, `NPM_TOKEN`, `HTTPS_PROXY` and any `OTEL_*`), matched without regard to case and with an `INPUT_` prefix, any `*_BASE_URL` whose value embeds credentials, and, beyond that list, every `ANTHROPIC_*` and `CLAUDE_*` variable, so that one a later Claude Code protects stays withheld while the list lags. With `--verbose`, a debug line names the variables withheld from each server, never their values |
| `cursor` | `${env:NAME}`, `${userHome}`, `${pathSeparator}`, `${/}` | stays as written, and so do `${workspaceFolder}` and `${workspaceFolderBasename}`, a user-scope declaration having no workspace |
| `gemini-cli` | `$VAR`, `${VAR}`, `${VAR:-default}` | stays as written, except in an environment value or a header, where it is empty |
| `windsurf` | `${env:NAME}`, and `${file:/path}` for the file's contents, trimmed | is empty; a file that cannot be read stays as written |
| `github-copilot` | `$VAR`, `${VAR}`, `${VAR:-default}`, and `~` at the start of `cwd` for the home directory | stays as written |
| `codex` | none; the environment enters through `env_http_headers` and `bearer_token_env_var` | |

A server on `sse` speaks the legacy HTTP+SSE transport of protocol revision `2024-11-05`. A `GET` to its URL with the declared headers and `Accept: text/event-stream` opens an event stream whose first `endpoint` event names, relative to the URL, where each JSON-RPC message is `POST`ed with the same headers; the answers arrive on the stream as `message` events. An endpoint on another scheme or host than the URL is refused before anything is sent to it, since the declared headers would follow. The stream is closed at the end. A `streamable-http` server whose opening `POST` is refused with HTTP 400, 404 or 405 and a body that is not a JSON-RPC error (which is how a current server refuses a method) is tried once more on this transport at the same URL, as the protocol's backwards compatibility asks; when that URL opens no event stream, the original refusal is the failure. No other URL is ever tried: a handshake reaches the server where its client does.

A server that cannot be started, exits, answers something that is not JSON-RPC, refuses a method with any other error or runs out of its budget is one warning, `<config file>: server <name>: <reason>`, that never carries an environment or header value; the scan continues and the server keeps its stored signature, or `none`. The exit code stays 0.

The occurrence of a failed declaration carries `handshake_error`: `message` is the reason of the warning, `hint` is what the user can do about it, and `reason` is one of:

| `reason` | When |
|---|---|
| `unauthorized` | the server answered HTTP 401 or 403. The handshake sends only the declared headers and never the sign-in an agent client holds, so a server behind OAuth always fails this way; the hint says so, or, for a Codex `bearer_token_env_var`, to set or renew that variable, or, when the declaration sets headers, that they were refused |
| `unreachable` | the connection failed, or the server answered HTTP 404 or 410 without a JSON-RPC error |
| `not_found` | the command of a local server does not exist |
| `timeout` | the budget ran out |
| `failed` | everything else: the server exited, answered something that is not JSON-RPC, another HTTP status or a JSON-RPC error, or its event stream named an endpoint on another origin |

In human output the server's row ends with the failure in words (`needs sign-in`, `unreachable`, `command not found`, `timed out`, `handshake failed`), after the tool count of a stored signature, and stderr closes with one `hint:` line per distinct hint, after the warnings.

Every item is serialised as its kind, name, description, input schema and output schema joined by `NUL`, the schemas as canonical JSON (object keys sorted, no whitespace, numbers as written, only the escapes JSON requires) and empty for anything but a tool or an absent schema. Items are sorted by kind, then name, then serialisation. The signature is SHA-256 over the serialisations joined by `NUL`, rendered as 64 lowercase hex characters; a server that exposes nothing has the signature of the empty list. A tool's hash is SHA-256 over its own serialisation, and its `id` is the identity `tool` over the server's logical id and that hash, so a tool that is byte-identical in two versions of one server is one node.

After the handshakes, the signatures taken are written to `handshakes.json` in agentx home under the exclusive lock, replacing the entry of each server handshaken and keeping every other: `{"<server logical id>": {"signature": "<hex>", "tools": [...], "at": "<RFC 3339>"}}`, `tools` as on the node. The file is derived state, so `version` is not rewritten: no other consumer needs to rescan for it. When one scan handshakes two declarations of one logical server that answer differently, each declaration gets its own node, and the file keeps the declaration last in scan order. A scan without `--handshake` reads the file and gives every declaration of a listed server the stored `signature`, `signature_at` and `tools`; a file that cannot be parsed is a warning and counts as empty.

Plugin discovery reads, per configuration:

| Configuration | Install records | Plugin directories | Manifest |
|---|---|---|---|
| `claude-code` | `~/.claude/plugins/installed_plugins.json`, keyed `<name>@<marketplace>`, each record with `installPath` and `version` | the `installPath` | `.claude-plugin/plugin.json`, for the version when the record has none |
| `codex` | the `[plugins."<name>@<marketplace>"]` tables of `~/.codex/config.toml`, each with `enabled` (`true` when absent) | `~/.codex/plugins/cache/<marketplace>/<name>/<version>/`; the active version is `local` when present, else the highest by semver, compared as strings where either name is not a semver | `plugin.json` at the root when its `$schema` starts with `https://agent-plugins.org/schemas/`, else the first of `.codex-plugin/plugin.json`, `.claude-plugin/plugin.json` and `.cursor-plugin/plugin.json` |
| `cursor` | none on disk: the account holds them | `~/.cursor/plugins/local/<dir>` and `~/.cursor/plugins/cache/<marketplace>/<name>/<version>/` holding a `.cache-complete` file | the first of `.cursor-plugin/plugin.json`, `.claude-plugin/plugin.json` and `plugin.json` |
| `gemini-cli` | none: every directory under `~/.gemini/extensions` holding a `gemini-extension.json` is an extension | that directory | `gemini-extension.json`, which also declares the `mcpServers`, with `${extensionPath}` replaced by the extension directory and `${/}` and `${pathSeparator}` by the path separator in the command, the arguments, the environment values and `cwd`; the marketplace is the `source` in `.gemini-extension-install.json` next to it, when present |

Every other configuration reports no plugins. For every client, a manifest or server file that cannot be parsed is one warning naming the file, never its content, and the plugin is still reported with what its directory gives; a missing file is silent. The skills a plugin provides are discovered in its skills directories exactly like a skills directory (direct children only), merged with every other skill by content hash and recorded as occurrences carrying the plugin's name; its servers are recorded as occurrences carrying the plugin's name with the file they were read from as `config_file`.

Claude Code: a record without an `installPath` or whose directory does not exist is a warning; skills are under `skills` and servers in `.mcp.json` at the root, in the `mcpServers` JSON format, with `${CLAUDE_PLUGIN_ROOT}` replaced by the plugin directory in the command, the arguments, the environment values and `cwd`.

Codex: the name and marketplace are the two halves of the `config.toml` key, split on its last `@`; a key without both halves is a warning, and a key whose plugin directory does not exist is a warning naming that directory. A plugin directory without a key, which is how a plugin installed through the account appears, is reported without an `enabled` field. The version is the manifest's `version`, else the version directory name. Skills are the manifest's `skills` paths (one string or a list, each starting with `./` and staying inside the plugin, else a warning naming the path), else `skills`. Servers are the manifest's `mcpServers` object, recorded with the manifest as `config_file`, or the file that field names, else `.mcp.json`, else `mcp.json`; the file holds the Codex `config.toml` fields (`command`, `args`, `env`, `cwd`, `url`, `http_headers`, `env_http_headers`, `bearer_token_env_var`, `enabled`) as JSON, either under `mcpServers` or as a bare map of name to declaration. A `[plugins."<name>@<marketplace>".mcp_servers.<server>]` table with `enabled = false` marks that server's occurrence `enabled: false`; its other keys are ignored. A plugin with `enabled = false` marks every server it provides the same way. In a plugin in the Agent Plugins format, `${PLUGIN_ROOT}` is replaced by the plugin directory in the command, the arguments, the environment values and `cwd`.

Cursor: a plugin under `plugins/local` has an empty `marketplace`; a hidden entry or a file there is skipped, and a symlink is followed only when it resolves inside `plugins/local`, else it is a warning naming the link. A cached plugin takes its `marketplace` and `name` from the directory names, the manifest's `name` replacing the latter when present, and its `version` from the manifest, else the version directory name (a commit id or `release_<tag>`); an entry without `.cache-complete` is a warning naming the directory and reports no plugin, and every complete version is reported. A plugin without a manifest is named after its directory. Cursor records neither what is installed nor what is enabled on disk, so a reported plugin is installed as far as the cache shows, not necessarily enabled, and its node has no `enabled` field. Skills are in the manifest's `skills` paths (one or a list, each relative, with or without a leading `./` as Cursor's own marketplace plugins omit it, and staying inside the plugin, else a warning naming the path; a path holding a `SKILL.md` is that one skill, another is a skills directory), else under `skills`; servers are the manifest's `mcpServers` object, with the manifest as the `config_file`, else the file the manifest names, else `mcp.json`, else `.mcp.json`, in the `mcpServers` JSON format, with `${CURSOR_PLUGIN_ROOT}` and `${CLAUDE_PLUGIN_ROOT}` replaced by the plugin directory in the command, the arguments, the environment values and `cwd`. The Claude Code plugins Cursor imports from `~/.claude/plugins` are reported under `claude-code` only, never a second time under `cursor`.

Discovery: inside each skills directory a client reads, every child directory, or symlink to one, that holds a `SKILL.md` is a skill; hidden entries and `node_modules` are skipped; a broken symlink is a warning naming the link, and several in one directory are one warning naming the directory, their count and their names. A client's user-scope skills directories are its own, other clients' directories it reads (Cursor reads the Claude Code and Codex directories) and the library for clients that read it directly (Codex and Gemini CLI), which yields an occurrence with the library path as placement path whatever the enabled state. Each directory is canonicalised once and each skill is hashed once per scan.

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

- Isolated, for every command that writes objects or merges, and for every local read of the account repo: `GIT_CONFIG_GLOBAL` points at `/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, every `GIT_*` variable of the user's is dropped, author and committer are fixed to `agentx <agentx@localhost>` at `946684800 +0000`, `GIT_NO_LAZY_FETCH=1` forbids fetching a missing object one at a time (git 2.45 and newer honour it; older gits ignore it), and `core.autocrlf=false`, `commit.gpgsign=false` and `core.hooksPath=/dev/null` are passed with `-c`. A commit made this way has the same id on every machine whatever the user's git configuration, and nothing in this environment touches the network.
- User, for network commands, today the fetches of `source add` and `source fetch`: the CLI's environment as is, so credential helpers, SSH configuration and URL rewrites (`url.<base>.insteadOf`) apply. agentx never stores git credentials.

In the serve child every git call additionally has `GIT_TERMINAL_PROMPT=0`, `-o BatchMode=yes` appended to the SSH command and `GIT_ASKPASS=/bin/false`, so a prompt becomes an error rather than a hang.

## Doctor

`agentx doctor` checks whether this machine can run agentx and reports one row per check, in this order. It only reads: it creates neither agentx home, nor the lock file, nor the account repo, and its git probes run in a throwaway repository in the temporary directory.

| Check | Statuses | What it means |
|---|---|---|
| `git` | `ok`, `fail` | git is in `PATH` and 2.40 or newer; the detail names the version |
| `fork_merges` | `ok`, `fail` | upstream changes can be merged into forks: `git merge-tree --write-tree --merge-base=<base> <ours> <theirs>` merges two branches of a throwaway repository in the temporary directory, made with deterministic commits |
| `commit_identity` | `ok`, `fail` | commits get the same id on every machine: the isolated environment gives a fixed input the known commit id `5d75017e77f5413f4337ef776244b8d8dc77ca90`; the failure detail names both ids |
| `home` | `ok`, `fail` | agentx home is writable, or does not exist yet; the first scan creates it |
| `lock` | `ok`, `warn` | the lock is free, a missing lock file included, or held by another agentx command; the detail names the holder's process id |
| `mutations` | `ok`, `warn`, `fail` | no [mutation journal](#mutation-journal) is unfinished; a warning names the count and the oldest journal, and the hint says how to recover; doctor recovers nothing itself; `fail` when the directory cannot be read |
| `settings` | `ok`, `fail` | `settings.json` parses, or does not exist yet; the detail and hint name the path |
| `account_repo` | `ok`, `fail` | the account repo opens, or does not exist yet |
| `library` | `ok`, `fail` | the library is a writable directory, or does not exist yet; the first install creates it |
| `client:<slug>` | `info` | one row per detected agent configuration, sorted by slug; the detail is `<name>: <configuration directory>` |
| `clients` | `info`, `warn` | `<n> of <m> registered clients detected`, where `m` is the size of the client registry; `warn` with a hint when nothing is detected |

`doctor`, one event per check:

| Field | Type | Meaning |
|---|---|---|
| `check` | string | the check name from the table above |
| `status` | string | `ok`, `warn`, `fail` or `info` |
| `detail` | string | what was found, naming the version, path or error involved |
| `hint` | string | how to fix it; absent when there is nothing to suggest |

Without `--json` the rows print in three titled sections, `System` (`git`, `fork_merges`, `commit_identity`, `home`), `App` (`lock`, `mutations`, `settings`, `account_repo`, `library`) and `Clients` (every `client:<slug>` row; the `clients` detail is shown after the section title rather than as a row), each row indented as `<glyph> <check>  <status>  <detail>`, the glyph being `✓` for `ok`, `!` for `warn`, `✗` for `fail` and `•` for `info`, with the columns aligned across sections. After the sections, a blank line and either `✓ No issues detected` or an `<n> issues` block (`1 issue`) listing every `warn` and `fail` row again as `<glyph> <check>  <detail>`, with its `hint` on the next line as `hint: <hint>` aligned under the detail. Exit code 2 when git is missing or too old; the run stops after the `git` row, since nothing else can be checked. Exit code 8 when the account repo is unusable, after every row. Otherwise 0, warnings included. The `result` event carries `ok: false` exactly when the exit code is non-zero.

## Account repo

The account repo is `account.git` in agentx home, a bare repository. It is created on first use, under the lock, by the first command that opens it: `agentx source add` today. Its creation bumps `version` like any mutation. On a fresh machine it does not exist and `agentx doctor` reports that. Creation runs `git init --bare` in the isolated environment and sets `gc.auto=0` (maintenance runs on the serve child's timer, never inside a command), `core.logAllRefUpdates=true` (reflogs, which a bare repository lacks by default), `merge.conflictStyle=zdiff3` and, on git 2.48 or newer, `worktree.useRelativePaths=true`. The repository is renamed into place only once every step succeeded. A present `account.git` that is not a bare repository git can read is exit code 8 with a hint naming the path.

Git config in the account repo holds only what git owns: the remotes of [Sources](#sources), and later tracking branches.

## Sources

A source is a git repository added by URL, from which skills are installed. `agentx source add <url>` fetches it, `agentx source fetch [<url|id>]... [--all]` fetches sources that are already added, `agentx source list` lists the sources of this machine, `agentx source skills <url|id>` lists the installable skills of a fetched source, and `agentx source remove <url|id>` deletes one. `add`, `fetch` and `remove` are mutations; `list` and `skills` read and never touch the network.

**URL forms.** `source add` and every `<url>` argument accept, with an optional `#<ref>` fragment on each: `owner/repo` and `owner/repo/<subpath>`, which resolve to GitHub; `https://github.com/owner/repo`, with or without `.git` or a trailing slash, followed by an optional `/<subpath>` or `/tree/<ref>/<subpath>`; a GitLab URL, `https://gitlab.com/group/subgroup/repo`, with an optional `/-/tree/<ref>/<subpath>`; the SSH shorthand `git@host:path` and `ssh://` and `git://` URLs; any `https://` or `http://` host; and `file://<path>`. Everything else, a GitHub `blob`, `pull` or other page URL included, is exit code 1 with a hint listing the forms. The result is the canonical URL, an optional subpath and an optional ref. The canonical URL is the source's identity: the scheme and host in lower case, the host without a trailing dot and without a port that is the scheme's default, an IP address host in one spelling and bracketed, `.git` and a trailing slash dropped, `git@host:path` written as `ssh://git@host/path`, the path's percent escapes kept as escapes, and a `file://` path cleaned but otherwise as it is on disk. The canonical URL of anything accepted parses back to itself, to the same id, and to the repository alone with no subpath and no ref: it is printed by `source list` and read back by `source remove` and `source skills`, so a form that did not round trip would name a different source than the one it came from. A pin must be a ref git accepts, which is what `git check-ref-format` allows for a one-level name, or a commit id; a name git would refuse is exit code 1 here rather than a fetch failure later. On GitHub the repository is `owner/repo` and what follows is the subpath; elsewhere the repository path ends at `/-/` or at the first segment named `*.git`, and what follows is the subpath, so `https://git.example.com/team/repo.git/skills` and `file:///srv/skills.git/tools` name a subpath and `https://git.example.com/team/repo/skills` is the repository `team/repo/skills`. The ref of a tree URL is the pin; a fragment naming a different ref is exit code 1. A ref must be a name git accepts for a branch or tag, or a commit id. The subpath scopes what the command lists and is never stored; a subpath that is not a directory of the repository at the fetched commit is exit code 5. A user or token embedded in the URL is removed before the URL is used anywhere: it never enters the settings, the account repo's configuration, a trailer or an event, and the command logs one warning saying so and naming the credential helper. For an `ssh://` URL the user name (`git@`) is part of the address and is kept; only a password is removed.

**Id.** The source id is the first 16 lowercase hex characters of SHA-256 over the canonical URL. It names the remote `src-<id>` and the ref `refs/agentx/sources/<id>` in the account repo and is accepted wherever a URL is.

**Fetch.** `source add` opens the account repo, creating it when needed, then writes the remote in the isolated environment: `remote.src-<id>.url` is the canonical URL, `remote.src-<id>.fetch` is `+<pin>:refs/agentx/fetching/<id>`, or `+HEAD:refs/agentx/fetching/<id>` for an unpinned source so that the remote's default branch is followed whatever its name; its left side records the pin and its right side is the staging ref, never the source ref, so that `refs/agentx/sources/<id>` is written by the `update-ref` that ends a whole fetch and by nothing else — not by a fetch agentx did not make, and not by a refspec an interrupted run left behind, `remote.src-<id>.tagOpt=--no-tags`, `remote.src-<id>.promisor=true` and `remote.src-<id>.partialclonefilter=blob:none`. It then runs `git fetch --quiet --no-tags --no-write-fetch-head --recurse-submodules=no --no-show-forced-updates --refmap= --filter=blob:none src-<id> +<pin>:refs/agentx/fetching/<id>` in the user environment. A fetch lands on the source's **staging ref** and never on the source ref: filling a source takes two network round trips, and between them there is a commit the account repo can read whose blobs are not here yet, which no reader of a source ref may ever see. The refspec is built from the pin the settings hold rather than left to `remote.src-<id>.fetch`, so that a remote a run was interrupted before it could write cannot make a fetch follow a ref the settings do not name; `--refmap=` is what makes it the only refspec, since one on the command line does not replace the configured one — git also updates that one opportunistically. The remote's configuration is written under the lock, since `git config` fails rather than waits when another command holds the repository's own config lock; the fetch itself runs outside it, so that a slow network never blocks a scan. The fetch brings every commit and tree of the ref and no blob; a server that does not support partial clone ignores the filter, which git reports as a warning, and the fetch is a full one. A pin that names no branch, tag or commit of the source is exit code 5; any other fetch failure, unreachable host, authentication, or not a repository, is exit code 3 with the first line of git's error and a hint naming the credential helper, worded for the dropped credential when the URL carried one. The staging ref is then read as a commit (an annotated tag is peeled; the object the ref names, tag or commit, is what the source ref will end up holding), and the skills under the subpath are listed from the trees, tree objects only: every directory holding a `SKILL.md` (a regular file; a symlink does not count), the root included, skipping any directory below the subpath whose name starts with `.` or is `node_modules`, at any depth, while a skill inside another skill's directory counts. The `SKILL.md` blobs are then fetched in one batch, by object id on the standard input of one `git fetch` carrying `-c fetch.negotiationAlgorithm=noop`, the flags above except `--no-show-forced-updates`, and `--refmap=`, `--filter=blob:none` and `--stdin`, the way git itself fills a partial clone; the batch names objects rather than refs and updates none, so it has no forced update to suppress; a server that serves the filtered fetch but refuses single objects gets one `git fetch --refetch --no-filter` with the same flags instead, which makes the fetch a full one. The batch covers every `SKILL.md` of the repository, the skipped directories included, not only those under this command's subpath: a subpath scopes one listing and is never stored, so every later listing, of any part of the source, is answered from the account repo without the network. No blob is ever fetched one at a time: every local read runs with `GIT_NO_LAZY_FETCH=1`. Every `SKILL.md` of the commit is then looked for in one `git cat-file --batch-check`; a batch a server answered without sending every object it was asked for is exit code 5 with a hint to fetch the source again, the source ref unmoved. The blobs are read in one `cat-file --batch`, and the skill list (subpath, name, description, tree id) is built in memory and never written to disk.

**Publishing a fetch.** Only once the commit is whole does `git update-ref refs/agentx/sources/<id> <fetched object>` move the source ref onto it, followed by `git update-ref -d refs/agentx/fetching/<id>`. `refs/agentx/sources/<id>` therefore never names a commit whose `SKILL.md` blobs are not in the account repo, and a fetch is invisible to every other reader — the [serve](#serve) child rebuilding its source index, a concurrent `source skills` or `source list`, the next command after a crash — until it is whole: each of them sees the commit the last complete fetch left, then the new one, and nothing in between. A fetch that fails at any step, for any cause, leaves the source ref exactly where it was, so a failed fetch never costs a machine the source it already had. The staging ref is deleted when the fetch ends, whether it finished or failed; only a killed process can leave one behind, and nothing reads it, the next fetch of that source overwrites it and `source remove` deletes it. Finally, under the lock, the settings entry is written: `url`, `pin` when set and `last_fetched` now; an existing entry for the URL is replaced, its `alias` kept, so adding a source again re-fetches it and changes its pin to what the new command said, a missing fragment included.

**A remote the settings do not name is not a source.** `source fetch` and `source skills` both answer from the settings, so a remote no entry names is a remote nothing will ever name again or clean up. It exists in the account repo only because an entry names it, or because an add is in flight for it. An add that wrote the remote and cannot write the entry therefore takes the remote back: a source that was not there before goes altogether, remote and ref, and one that was there goes back to the pin the settings still hold, which is what the next command will read. The same take-back answers a failed fetch, which also leaves a remote written and nothing recording it. It runs under the lock: where the settings write held the lock, inside that same hold, and where the settings write never won it, as a [take-back](#lock-and-version-file) that waits for the lock rather than giving up on it. The run still ends with the exit code of whatever stopped it, a lost lock still being code 7. A take-back that fails even then is reported rather than passed over: the message names the remote left in the account repo and says no source names it, and the hint gives the two repairs — `agentx source add <url>` again, which rewrites the remote and writes the entry, or `agentx source remove <id>`, which clears the remote. A take-back is itself a mutation, so an earlier mutation's journal that cannot be recovered refuses it exactly as it refuses any other command, whatever the take-back waits for: the remote then stays in the account repo, reported the same way, until the journal is resolved and the add is run again. The first hold of the lock, the one that writes the remote, is not a take-back and gives up like any other mutation: a run that loses it has written nothing.

**Fetch again.** `agentx source fetch [<url|id>]... [--all]` fetches sources that are already added, so that a source no skill was installed from, which nothing else refreshes, can be brought up to date: several arguments fetch several sources and `--all` fetches every source of the machine. Neither an argument nor `--all` is exit code 1 naming both forms, and so is giving both at once. Every argument is resolved against the settings before anything is fetched: one that names no added source is exit code 5 with a hint to add it, a subpath in a URL argument is ignored, since a fetch covers the whole source whatever part of it is named, and a `#<ref>` other than the entry's pin is exit code 1 naming the stored pin, the rule `source skills` follows; a source named twice, by URL and by id, is fetched once. Each source is fetched at the pin the settings hold, through the remote `source add` configured, by the fetch above and nothing else: the blobless fetch onto the staging ref, then the `SKILL.md` blobs of the whole repository in one batch, then the source ref. Each source stages on its own `refs/agentx/fetching/<id>`, so the parallel fetches of one run never share a ref, and a source that fails leaves its own source ref where it was without disturbing any other. A fetch writes the remote only to bring it back in line with the settings: the remotes of the run's sources are read in one `git config --get-regexp` under the early lock, and the refspec of one that does not record the pin the settings hold is rewritten, which a run interrupted between the remote and the settings leaves behind. A source with no remote at all is left alone rather than given half of one. That changes no pin — the pin is what the settings say, and only `source add` sets it. An account repo that does not exist is exit code 5 with a hint to add the source, nothing having been fetched on this machine at all, and so is a source whose remote the account repo no longer holds, which an interrupted removal leaves: git answers a fetch through a remote that is not there by naming that remote, which is agentx's own name for the source, and the run says what `source skills` says of the same state instead. The sources are fetched in parallel, at most four at a time as the handshakes of a scan are, outside the lock, and each one ends with a `progress` event of phase `fetch` carrying its URL. A source that could not be fetched is one warning naming it and the reason, and the run goes on: every other source still lands. The lock is taken once before the network, to recover an unfinished [mutation journal](#mutation-journal) and to refuse a run that could not write the settings anyway, and once at the end for one write of `last_fetched`, now, for every source that fetched. A source removed while the run fetched has no entry to write, is left out of the report rather than named as present and fresh, and has the ref its fetch published taken away again under that same lock, so that a removal a fetch raced still leaves nothing of the source. That write bumps `version` like any mutation, so the [serve](#serve) child rebuilds its source index after a fetch that moved a ref, and a fetch that moved nothing still updates `last_fetched`. One run over several sources has one exit code, and it is the code every failure agrees on: fetching one source answers exactly as adding or listing that source would, and a run whose sources all failed the same way answers for that cause. Exit code 3, the source-level refusal, is what a run whose causes disagree answers, no one code being true of them all. The `error` and `result` events name every source that failed, the `hint` answers the `code`, and each failure is one `warning:` line on stderr naming its source. `--all` on a machine with no source prints what `source list` prints for none and exits 0.

**Listing.** `source skills` resolves its argument to an entry of the settings, exit code 5 when there is none, with a hint to add it; a URL argument may carry a subpath, and a fragment naming a ref other than the entry's pin is exit code 1. It reads `refs/agentx/sources/<id>` (exit code 5 when the account repo holds no ref for the source, and exit code 5 with a hint to fetch the source again when an object the listing needs is missing) and lists the skills as above from the account repo alone. A local git failure in any `source` command is exit code 8, and so is a `source fetch` run over several sources when every source failed that way; it is never reported as a source that was not fetched: the account repo holding no ref for the source is exit code 5, git failing to read the refs at all is exit code 8, and each hint leads to the command that addresses that cause. `source list` reads the settings and runs two git processes: the `rev-parse --is-bare-repository` that checks the account repo, then one `for-each-ref` over `refs/agentx/sources/`.

**Removal.** `source remove` deletes the remote (`git remote remove src-<id>`), the source ref and the staging ref a killed fetch can have left (`git update-ref -d` each), then the settings entry, all under the lock, so that nothing of the source is left under `refs/agentx`. The git state goes first: were the entry deleted first and the git deletion then failed, nothing would name the source any more and its remote would be unreachable for good. The objects stay until maintenance reclaims them. A managed skill installed from the source keeps its coordinates. Adding the source again fetches it anew.

Without `--json`: `source add` prints `✓ added <url> at <short commit>: <n> skills` for a source that was not there before, and for one that was, `✓ re-fetched <url>, now at <short commit>, was <short commit>`, or `, already at <short commit>` when the fetch changed nothing, so a repeat says what moved rather than repeating that it exists; ` pinned to <ref>` follows the URL when pinned and ` under <subpath>` ends the line when scoped; `source fetch` prints one of those `re-fetched` lines per source that fetched, in the order the command named them, its arguments in their order and `--all` in the order `source list` prints, whatever order the fetches themselves finished in, and nothing for a source that failed, which is a `warning:` line on stderr instead; `source list` prints `<n> sources` and one row per source, `<url>  <pin or (unpinned)>  <short commit or not fetched>  <last_fetched>  <id>`, or `No sources. Add one with agentx source add <url>.`; `source skills` prints `<n> skills in <url>[ under <subpath>] at <short commit>` and one row per skill, `<name>  <subpath or .>  <description>`, the three of them sanitised as [Streams](#streams) prescribes, since the source writes all three; `source remove` prints `✓ removed <url>`. A short commit is its first seven characters. Every row of the `source list` and `source skills` listings is indented two spaces; the `re-fetched` lines of `source fetch` are one per source and are not indented, being confirmations rather than rows of a listing.


## Serve

`agentx serve --json` is the long-running child the desktop app holds open. It scans once on start, emits that snapshot, then rescans when the machine changes and emits the snapshot again only when the inventory changed. It runs until its stdin closes or its context is cancelled, then emits `result` and exits 0. Without `--json` it prints one line per event: `snapshot <counter>: <n> configurations, <n> skills`, `refresh <request_id>: ok, snapshot <counter>`, `refresh <request_id>: failed: <error>` or `search <request_id>: <n> results` (`1 result`).

One serve child per agentx home: on start it takes an exclusive advisory `flock` on `serve.lock` in agentx home and holds it until it exits. A second `agentx serve` for the same home, `--once` included, exits at once with code 6 and a hint naming `serve.lock`. This lock is distinct from `lock`: serve never blocks a mutating command.

Snapshots: every serve process has a fresh `instance_id` (or `AGENTX_INSTANCE_ID`). The initial scan always emits a `snapshot` with `scan_counter` `1`, even when an earlier process saw identical content. Each later scan is compared with the last emitted snapshot on its serialised bytes with `instance_id` and `scan_counter` excluded; an identical snapshot emits nothing, a different one increments the counter and is emitted whole. The desktop applies snapshots only from its active child and only with increasing counters, as [snapshot ordering](snapshot-ordering.md) prescribes.

Scans: every scan holds the shared `flock` on `lock` over its local reads, like `agentx scan`, but waits for a mutation in progress for as long as it takes instead of giving up after a second; it takes the exclusive lock only to recover an unfinished [mutation journal](#mutation-journal). Scans are serialised, one at a time. A change signal that arrives during a scan schedules one more scan after it.

Change signals: serve watches, in this order, agentx home (where every mutating command rewrites `version` last), `account.git`, `worktrees`, the library and every user-scope skills directory a registered client reads, and then every directory below each of those trees, so an edit inside a skill is a signal too, wherever the scan reads that skill from. The skills directories come from the whole registry, not from the detected clients alone, so a client whose configuration appears while serve runs is covered by the sync that first sees its directory; a directory no detected client reads costs a rescan that finds the inventory unchanged and emits nothing. Directories are watched on their real paths after resolving symlinks, and a real path reached twice, as a library symlink to a fork worktree is, as the library itself is for every client that reads it directly, and as the Claude Code directory is for Cursor, is watched once. Hidden directories and `node_modules` are skipped, as in discovery. Before every rescan the watches are brought in line with the directories that exist then: one that appeared, or did not exist at start, is watched from then on and one that disappeared is not. How the watching is done depends on the platform. On macOS, built with cgo as the release binary is, one FSEvents stream covers every watched directory and everything below the trees, at no cost per file, plus every directory a symlink below a tree leads out of it, since FSEvents does not follow symlinks; a change is a signal when it happens in a directory the rules above cover. Everywhere else, and on macOS built without cgo, each directory gets its own non-recursive watch: one inotify watch on Linux, where a library of a few hundred skills and the client directories beside it stay within the default limit, and on macOS without cgo one kqueue descriptor per directory and per file in it, which meets the default limit of 256 open files early. A placement that is a symlink into the library resolves to a path already watched and costs nothing; a placement that is a copy is watched on its own. Events are debounced: a rescan runs 100 ms after the last event, or 500 ms after the first event of a burst, whichever comes first. Watching is a precondition of serve: when the watcher cannot be created, or a directory cannot be watched at start or later, serve ends with exit code 6 and an `error` event whose message names the directory and the cause, after any snapshot already emitted; the hint names the limit to raise. Serve never rescans on a timer.

Stdin carries requests, one JSON object per line: `refresh` and `search`. A line that is not a JSON object, a request without a known `type`, a refresh without a `request_id`, or a search without a `request_id` or without a `query` is answered with an `error` event with code `usage` on stdout, and serve keeps running; under serve an `error` event therefore does not announce the end of the run. Blank lines are ignored.

`{"type": "refresh", "request_id": "<unique id>"}` asks for a scan. A refresh is satisfied only by a scan that begins after the request was received; a scan already in progress cannot satisfy it, and every request received before the next scan begins shares that scan. After the scan, a changed snapshot is emitted first, then one `refresh_complete` per request.

`refresh_complete`:

| Field | Type | Meaning |
|---|---|---|
| `request_id` | string | the id from the request |
| `instance_id` | string | this serve process's instance id |
| `ok` | boolean | whether the scan succeeded |
| `scan_counter` | integer | the counter of the current snapshot, changed or not; absent when `ok` is `false`, since a failed scan claims no freshness |
| `error` | string | why the scan failed; present only when `ok` is `false` |

A failed rescan is also reported as a `log` warning; the last snapshot stays in force and serve keeps running. A failed initial scan ends serve with the error's exit code.

`{"type": "search", "request_id": "<unique id>", "query": "<text>"}` searches the skills of every source in [machine settings](#machine-settings), so that the app never filters a listing itself. It is answered with one `search` event:

| Field | Type | Meaning |
|---|---|---|
| `request_id` | string | the id from the request |
| `instance_id` | string | this serve process's instance id |
| `query` | string | the text from the request |
| `results` | array | one object per matching skill: `source`, `subpath`, `name`, `description` and `tree`; empty when nothing matched |

A skill matches when its name or its description contains the query, compared case-insensitively. Results are sorted by source canonical URL, then name, then subpath. `source` is the canonical URL rather than the source id that `source_skill` carries: a search spans every source, and the URL is what names one to a person and what an install takes. `subpath`, `name`, `description` and `tree` are the skill as `source skills` lists it, `subpath` being `""` for a skill at the repository root.

Serve answers from the source index it holds in memory: one skill list per source in the settings, built from that source's ref `refs/agentx/sources/<id>` in the account repo exactly as `agentx source skills` builds its listing, over the whole source rather than a subpath, since a subpath scopes one listing and is never stored. The index is built after the initial scan and rebuilt after every later one, each time after that scan's snapshot has gone out and before its `refresh_complete` events, so that a snapshot never waits for it. Building it reads the settings under the shared `lock`, as a scan does, and reads the sources and the commits their refs hold in one `for-each-ref`; a source still at the commit the last build read it at keeps that build's skills rather than being listed again, so when every source is, and none has gone or arrived, the index stands unchanged and nothing else runs, and a rescan that follows a change elsewhere on the machine costs one git process while a machine with no source costs none. A source the last build could not list is listed again whatever its commit: that failure is the account repo's answer at that moment, not a property of the commit, so an index built while a source was momentarily unreadable recovers on the next build instead of holding that source empty until its ref moves. This is how a source added, fetched again or removed reaches the index: each of those is a mutation of agentx home, whose change signal schedules the scan, and a refresh acknowledged after it is answered from the new index. A source whose ref the account repo does not hold, and one it cannot list whole, contribute no skills and are one `log` warning per build.

A search takes no lock, runs no git process and makes no network call. It is answered from the index as it stands, while a scan is in progress, without waiting for it, so a search sent after a refresh can be answered before that refresh's `refresh_complete`.

`agentx serve --once` takes the serve lock, runs the initial scan, emits its snapshot and exits 0 with exactly the events `snapshot` and `result`. It watches nothing and reads no requests.
