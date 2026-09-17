---
status: accepted
date: 2026-09-16
---

# The Go CLI owns the machine and the account; the Tauri app only renders

agentx has two executables that ship together. The Go CLI, `agentx`, is the only component that reads or writes the machine, runs git, computes identities, signs in, and syncs. The desktop app is a Tauri 2 shell with a React frontend that spawns the CLI, reads its structured output and renders it. The app never scans, never parses a config file, never computes an identity, never opens a network connection. The CLI is a single static binary that is complete on its own from a terminal or CI.

## Contract between the two

The app spawns the CLI as a subprocess. Standard output carries newline-delimited JSON when the CLI is given `--json`; each line is one event with a `type` and a `schema_version`. Standard error carries logs. Without `--json` the same commands print for humans.

Two invocation modes. One-shot commands perform actions: `scan`, `doctor`, `version`, `skill add|new|fork|rename|update|check|diff|resolve|revert|remove|list|place|repair|history|commit`, `source add|list|skills|remove`, `adopt`, `config`, `remote`, `publish`, `pull`, `export`, `import`, `machine`; V1 adds `login|logout|whoami`. A long-lived `agentx serve --json` child, spawned once at app startup, runs the sync loop, executes operations queued for this machine, watches the library and the fork worktrees for edits, and streams events until the app exits. The serve child and one-shot commands share the same SQLite database in WAL mode, so a terminal user and the app can act at the same time.

Exit code zero means success. Exit codes two and above are operational failures mapped to a documented table. The app reads the exit code before trusting output. The app owns the child's lifecycle: it sets deadlines, puts children in their own process group, and kills the tree on cancel. A crash in the CLI never takes down the app. The app resolves the CLI next to its own executable first, then on PATH, and refuses to run against an incompatible schema major version.

## Why

Every action is scriptable and testable without a GUI, and terminal users get exactly what app users get. A new agent client is one Go file plus fixtures and a schema bump, with no app change unless it wants to render something new. Sign-in and sync in the CLI mean the app holds no credentials and no network code.

## Considered and rejected

Putting sync and sign-in in the Rust shell: two implementations of account logic, and the CLI would be a second-class citizen that cannot sync. Embedding Go in Rust via FFI: no failure isolation and the CLI stops being independently usable. A remote analysis service for identity derivation: identities are deterministic hashes, there is nothing to protect, and it would make the offline single-machine product incomplete.

## Consequences

The app depends on a compatible CLI being present; version checking at startup is mandatory. The CLI must expose everything the UI needs, including progress events for long operations, because the UI has no other way to learn anything.
