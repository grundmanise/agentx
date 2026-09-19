---
status: accepted
date: 2026-09-16
---

# The Go CLI owns the machine and the account; the Tauri app only renders

agentx has two executables that ship together. The Go CLI, `agentx`, is the only component that reads or writes the machine, runs git, computes identities, signs in, and syncs. The desktop app is a Tauri 2 shell with a React frontend that spawns the CLI, reads its structured output and renders it. The app never scans, never parses a config file, never computes an identity, never opens a network connection. The CLI is a single static binary that is complete on its own from a terminal or CI.

## Contract between the two

The app spawns the CLI as a subprocess. Standard output carries newline-delimited JSON when the CLI is given `--json`; each line is one event with a `type` and a `schema_version`. Standard error carries logs. Without `--json` the same commands print for humans. The [CLI contract](../spec/cli-contract.md) defines the event envelope, the event types, the exit code table and the layout of agentx home.

Two invocation modes. One-shot commands perform actions: `scan`, `doctor`, `version`, `skill add|new|fork|rename|update|check|diff|resolve|revert|remove|list|place|repair|history|commit`, `source add|list|skills|remove`, `adopt`, `config`, `remote`, `publish`, `pull`, `export`, `import`, `machine`; V1 adds `login|logout|whoami` and `skill unfork`. A long-lived `agentx serve --json` child, spawned once at app startup, talks to the account service, executes operations queued for this machine, watches the library and the fork worktrees for edits, and streams events until the app exits. The CLI keeps no database. Mutating commands take one advisory lock in agentx home, so a terminal user and the app can act at the same time; a command that finds the lock held exits with its own code. The serve child holds the snapshot in memory, learns of a terminal command's change from a mutation counter file that every mutating command bumps, and emits the whole snapshot again, with a process-local scan counter. The desktop applies snapshots only from its active serve child, identified by a fresh instance id and counters ordered only within that instance. One-shot command outcomes trigger a serve refresh with an acknowledgement even when inventory is unchanged. After a restart, the old inventory stays visibly stale until the new child emits its initial snapshot. The [snapshot-ordering contract](../spec/snapshot-ordering.md) defines refresh and read consistency. It also accepts request lines on its standard input for queries that need its in-memory state, such as search and refresh, and answers them with events; one serve child runs per agentx home.

Exit code zero means success. Exit codes two and above are operational failures mapped to a documented table; seven means another agentx command holds the lock, eight means the account repo is unusable. The app reads the exit code before trusting output. The app owns the child's lifecycle: it sets deadlines, puts children in their own process group, and kills the tree on cancel. A crash in the CLI never takes down the app. The app resolves the CLI next to its own executable first, then on PATH, and refuses to run against an incompatible schema major version. Before spawning the CLI the app imports the user's login shell environment, so git credential helpers and SSH agents behave as they do in a terminal.

The CLI runs git two ways. Commands that write objects or merge run with the user's global and system git configuration excluded and an explicit author, so commit ids are the same on every machine. Commands that touch the network run in the user's environment so their credentials apply. In the serve child every git call has terminal prompts disabled and fails instead of waiting for input.

## Why

Every action is scriptable and testable without a GUI, and terminal users get exactly what app users get. A new agent client is one Go file plus fixtures and a schema bump, with no app change unless it wants to render something new. Sign-in and the account service client in the CLI mean the app holds no credentials and no network code. The two modes remain independently usable. The mutation counter signals changes; it does not establish a consistent scan or an ordering across processes. Shared scan locks, explicit refresh acknowledgements and active-child identity provide that coordination without making the standalone CLI depend on a daemon.

## Considered and rejected

Putting sync and sign-in in the Rust shell: two implementations of account logic, and the CLI would be a second-class citizen that cannot sync. Embedding Go in Rust via FFI: no failure isolation and the CLI stops being independently usable. A remote analysis service for identity derivation: identities are deterministic hashes, there is nothing to protect, and it would make the offline single-machine product incomplete.

## Consequences

The app depends on a compatible CLI being present; version checking at startup is mandatory. The CLI must expose everything the UI needs, including progress events for long operations, because the UI has no other way to learn anything. Every command must stay fast on its own: a command spawns a bounded number of git processes, never one per skill, and computes tree and blob ids in process. Startup and doctor require Git 2.40 or newer, compare numeric version components, and probe `merge-tree --write-tree --merge-base` with deterministic commits. Where local state lives is decided in ADR 0004.
