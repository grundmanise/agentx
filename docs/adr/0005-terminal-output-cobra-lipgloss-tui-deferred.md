---
status: proposed
date: 2026-09-20
---

# Terminal output: cobra for commands, lipgloss for styling, no terminal user interface before v2

The terminal is a secondary surface. The desktop app is the primary one and drives the CLI through `--json` and `agentx serve`, which speak newline-delimited JSON. Text output exists for a developer who runs a command by hand, and it must read at a glance without becoming a product of its own.

## Decision

| Concern | Choice | Why |
|---|---|---|
| Command tree, flags, help | [cobra](https://github.com/spf13/cobra) | Already in place; the Charm command-line tools are built on it too, so nothing below replaces it. |
| Colour, width measurement, tables | [lipgloss](https://github.com/charmbracelet/lipgloss), one renderer per injected stream; under evaluation | The hand-written layer in `internal/cli/ui.go` measures painted text and pads columns itself; lipgloss does that with correct wide-character widths. The colour decision stays ours either way: on when the stream is a terminal, `NO_COLOR` unset and `TERM` not `dumb`; `--color on` or `off` overrides. |
| Help and error pages | The renderer in `internal/cli/help.go`; [fang](https://github.com/charmbracelet/fang) rejected | fang 1.0 was prototyped. Its `Execute` returns the error and honours cobra's out stream, so exit codes and stream routing would have worked. But it decides colour from the process (`os.Environ()`, a terminal check on the real stdout, a background-colour query written to stdout on every help and error, even in JSON mode) with no option to inject the environment, the profile or the width. So `--color on` cannot paint a pipe, `--color off` and `NO_COLOR` still leave decoration on a terminal, and the harness cannot test any of it. Its error page (`ERROR` badge, "Try --help") also differs from the contract's `error:`/`hint:` lines. It adds 24 modules to replace 116 lines. Revisit only if fang gains injectable environment and streams. |
| Logging | The `writer` in `internal/cli/events.go` | The contract fixes stderr as `level: message` in text mode and a `log` event with `schema_version` in JSON mode; a logging library would need a custom formatter to match it, which is more code than the two methods it would replace. |
| Markdown rendering | [glamour](https://github.com/charmbracelet/glamour), when a command first shows markdown | No command renders markdown yet. The first one that shows a `SKILL.md` body, a plugin README or a source description in the terminal should render it with glamour rather than printing raw markdown. Until then it is not a dependency. |
| Interactive terminal user interface | [bubbletea](https://github.com/charmbracelet/bubbletea), deferred to v2 | Nothing in v1 is interactive: every command prints and exits, and `agentx serve` is driven by the desktop app through stdin, so it must never take over the terminal. When a terminal user interface is built, it shares the lipgloss styles with the one-shot commands. |

## Consequences

- Text output keeps its contract in `docs/spec/cli-contract.md`: a pipe receives the aligned text without escape sequences, and stripping the sequences from coloured output gives exactly that text. Tests drive `cli.Run` with in-memory streams and assert on the plain text, so no library may read `os.Stdout` or the process environment on its own; lipgloss is used through `lipgloss.NewRenderer` on the injected stream, never through its package-level renderer.
- A `--json` consumer never sees colour, whatever the terminal or `--color` says.
- Adding a Charm library is a per-concern decision, made when the concern appears, not a platform choice made up front.
