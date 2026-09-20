---
status: accepted
date: 2026-09-20
---

# Terminal output: cobra for commands, lipgloss for styling, no terminal user interface before v2

The terminal is a secondary surface. The desktop app is the primary one and drives the CLI through `--json` and `agentx serve`, which speak newline-delimited JSON. Text output exists for a developer who runs a command by hand, and it must read at a glance without becoming a product of its own.

## Decision

| Concern | Choice | Why |
|---|---|---|
| Command tree, flags, help | [cobra](https://github.com/spf13/cobra) | Already in place; the Charm command-line tools are built on it too, so nothing below replaces it. |
| Colour and width measurement | [lipgloss](https://github.com/charmbracelet/lipgloss) v1, one renderer per injected stream | The palette is a set of lipgloss styles in the basic ANSI colours 0 to 7, and column widths come from `lipgloss.Width`, which measures wide characters correctly where the hand-written counter did not. The colour decision stays ours: on when the stream is a terminal, `NO_COLOR` unset and `TERM` not `dumb`; `--color on` or `off` overrides. The renderer's profile is therefore fixed explicitly, both when the output is created and on the renderer, so lipgloss never detects one from the process environment or the stream. Styles keep tabs untouched, so a painted fragment carries the exact text a pipe receives. This is not a simplification today: it swaps about 25 hand-written lines for 13 modules. It is taken because the terminal user interface planned for v2 will be built on the same styles, and the cost is paid once. |
| Tables | The small `table` type in `internal/cli/ui.go` | `lipgloss/table` v1.1.0 was tried on the doctor and settings rows with every border off and a two-space gutter. It pads the last cell of every row to its column width, so a pipe receives trailing spaces, and it clips the last row of every table: `computeHeight` subtracts a line for a border that is not drawn, and the render is capped at that height. Keeping a hidden border avoids the clipping but adds a blank line above and below and a leading space on every line. The type is about 35 lines that pad columns and measure with lipgloss; it is the one place alignment happens. |
| Help and error pages | The renderer in `internal/cli/help.go`; [fang](https://github.com/charmbracelet/fang) rejected | fang 1.0 was prototyped. Its `Execute` returns the error and honours cobra's out stream, so exit codes and stream routing would have worked. But it decides colour from the process (`os.Environ()`, a terminal check on the real stdout, a background-colour query written to stdout on every help and error, even in JSON mode) with no option to inject the environment, the profile or the width. So `--color on` cannot paint a pipe, `--color off` and `NO_COLOR` still leave decoration on a terminal, and the harness cannot test any of it. Its error page (`ERROR` badge, "Try --help") also differs from the contract's `error:`/`hint:` lines. It adds 24 modules to replace 116 lines. Revisit only if fang gains injectable environment and streams. |
| Logging | The `writer` in `internal/cli/events.go` | The contract fixes stderr as `level: message` in text mode and a `log` event with `schema_version` in JSON mode; a logging library would need a custom formatter to match it, which is more code than the two methods it would replace. |
| Markdown rendering | [glamour](https://github.com/charmbracelet/glamour), when a command first shows markdown | No command renders markdown yet. The first one that shows a `SKILL.md` body, a plugin README or a source description in the terminal should render it with glamour rather than printing raw markdown. Until then it is not a dependency. |
| Interactive terminal user interface | [bubbletea](https://github.com/charmbracelet/bubbletea), deferred to v2 | Nothing in v1 is interactive: every command prints and exits, and `agentx serve` is driven by the desktop app through stdin, so it must never take over the terminal. When a terminal user interface is built, it shares the lipgloss styles with the one-shot commands. |

## Consequences

- Text output keeps its contract in `docs/spec/cli-contract.md`: a pipe receives the aligned text without escape sequences, and stripping the sequences from coloured output gives exactly that text. Tests drive `cli.Run` with in-memory streams and assert on the plain text, so no library may read `os.Stdout` or the process environment on its own; lipgloss is used through `lipgloss.NewRenderer` on the injected stream, never through its package-level renderer.
- A `--json` consumer never sees colour, whatever the terminal or `--color` says.
- Importing lipgloss links termenv, whose package initialisation probes the real stdout once at startup. That probe never reaches the output, since every style is rendered through the renderer of the injected stream, but it is a process-level read that exists only because the library is linked.
- Adding a Charm library is a per-concern decision, made when the concern appears, not a platform choice made up front.
