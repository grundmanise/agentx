# agentx desktop

The desktop app: a Tauri 2 window with a React and TypeScript frontend. It renders what the `agentx` CLI reports and runs CLI commands for every action. It never reads the machine, runs git or opens a network connection itself; see [ADR 0001](../../docs/adr/0001-go-cli-owns-machine-and-account-tauri-renders.md).

## What is here

| Path | What it holds |
| --- | --- |
| [`DESIGN.md`](DESIGN.md) | The design system: colour, type, spacing and radius tokens, components, motion and copy rules |
| `src/styles/tokens.css` | The tokens as CSS variables, generated from `DESIGN.md` |
| `src/app` | The app shell: title bar, sidebar, main panel, command palette |
| `src/screens` | One folder per screen |
| `src/components` | Shared components; `ui` holds the shadcn/ui-based primitives |
| `src-tauri` | The Rust side: the window, and later the CLI supervisor |

## Develop

You need Node 22.22.2 or later, pnpm 10 and Rust. On Linux, Tauri also needs the WebKitGTK development packages ([Tauri prerequisites](https://v2.tauri.app/start/prerequisites/)).

```sh
cd apps/desktop
pnpm install
pnpm tauri dev   # the app in its window
pnpm dev         # the frontend alone, in a browser at http://localhost:1420
```

| Command | What it does |
| --- | --- |
| `pnpm test` | Checks the tokens are in sync with `DESIGN.md`, then runs the tests |
| `pnpm typecheck` | Type-checks the frontend |
| `pnpm build` | Builds the frontend into `dist` |
| `pnpm tokens` | Regenerates `src/styles/tokens.css` after you edit `DESIGN.md` |

From the repository root, `make desktop-check` runs what CI runs for the frontend, and `make desktop-rust-check` checks the Rust side.

## Rules for components

- Use the semantic tokens from `DESIGN.md` through their Tailwind names (`bg-shell-content-bg`, `rounded-card`, `w-(--shell-sidebar-width)`). Never use a primitive, a hex value or a default Tailwind colour; a test fails when a component does.
- Follow the copy rules in `DESIGN.md` §9 and the vocabulary in [`CONTEXT.md`](../../CONTEXT.md).
- Show only what the CLI reports. A screen never predicts the result of a command.
