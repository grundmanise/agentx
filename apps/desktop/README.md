# agentx desktop

The desktop app: a Tauri 2 window with a React and TypeScript frontend. It renders what the `agentx` CLI reports and runs CLI commands for every action. It never reads the machine, runs git or opens a network connection itself; see [ADR 0001](../../docs/adr/0001-go-cli-owns-machine-and-account-tauri-renders.md).

## What is here

| Path | What it holds |
| --- | --- |
| [`DESIGN.md`](DESIGN.md) | The design system: colour, type, spacing and radius tokens, components, motion and copy rules |
| `src/styles/tokens.css` | The tokens as CSS variables and `type-*` utilities, generated from `DESIGN.md` |
| `.oxlintrc.json`, `lint/` | The design-system lint and its local rules; `lint/fixtures` holds the code it must reject |
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
| `pnpm lint` | Runs the design-system lint on `src` |
| `pnpm build` | Builds the frontend into `dist` |
| `pnpm tokens` | Regenerates `src/styles/tokens.css` after you edit `DESIGN.md` |

From the repository root, `make desktop-check` runs what CI runs for the frontend, and `make desktop-rust-check` checks the Rust side.

## Rules for components

These rules are for anyone writing a component, people and coding agents alike. If a value is not a decision recorded in `DESIGN.md`, the code should not pass `pnpm lint` or `pnpm test`.

- Style with the semantic tokens from `DESIGN.md` through their Tailwind names: colours (`bg-shell-content-bg`), radii (`rounded-card`), shadows (`shadow-overlay`), layout tokens (`w-(--shell-sidebar-width)`, `p-(--page-padding)`) and type roles (`type-page-title`, then a colour such as `text-text-primary`). Tailwind's default colours, radii, shadows, font sizes, line heights and tracking do not exist in this app, so a class that names one does nothing, and the lint reports it as unknown.
- A gap or size that no token lists uses Tailwind's spacing steps in whole or half steps, a 2px grid (`gap-2.5`, `h-9.5`).
- No escape hatches. The lint rejects arbitrary values (`text-[13px]`, `rounded-[10px]`, `bg-[#fff]`), arbitrary properties, alpha modifiers on colours (`bg-card-bg/50`), `font-mono`, `leading-*` and `tracking-*` outside a type role, `rounded-full`, and inline styles. A value known only at run time goes in a custom property (`style={{ "--progress": n }}`) that a class reads (`w-(--progress)`).
- When the design needs a value no token holds, add the token to `DESIGN.md` in the table for its element, run `pnpm tokens` and use it. Do not reuse another element's token because the value matches.
- Screens and the app shell build on the components in `src/components`: native `<button>`, `<input>`, `<textarea>` and `<select>` are allowed only there. Use `Button` and its variants; add a component, or a variant to an existing one, when none fits.
- The lint reads classes in `className`, `cn()` and `cva()`. A class string anywhere else (such as a library's `classNames` option) goes through `cn()` so it is checked too.
- Components take typed props for their choices (`Button`'s `variant` and `size`, `Icon`'s `size`), not free values.
- Code generated with the shadcn CLI uses shadcn's default classes; map them to tokens before committing, or the lint fails.
- Follow the copy rules in `DESIGN.md` §9 and the vocabulary in [`CONTEXT.md`](../../CONTEXT.md).
- Show only what the CLI reports. A screen never predicts the result of a command.
