# agentx desktop

The desktop app: a Tauri 2 window with a React and TypeScript frontend. Today it is the app shell with placeholder screens. By design it will only render what the `agentx` CLI reports and run CLI commands for every action; it never reads the machine, runs git or opens a network connection itself. See [ADR 0001](../../docs/adr/0001-go-cli-owns-machine-and-account-tauri-renders.md).

## What is here

| Path | What it holds |
| --- | --- |
| [`DESIGN.md`](DESIGN.md) | The design system: colour, type, spacing and radius tokens, components, motion and copy rules |
| `src/styles/tokens.css` | The tokens as CSS variables and `type-*` utilities, edited by hand. It also resets Tailwind's theme so only these tokens exist |
| `oxlint.config.ts`, `oxfmt.config.ts` | The lint and the formatter: [Ultracite](https://www.ultracite.ai)'s presets for oxlint and oxfmt, plus the design-system lint built on [`@shadcn/lint`](https://github.com/shadcn-ui/lint) |
| `lint/fixtures` | The off-system code the design-system lint must reject |
| `src/app` | The app shell: title bar, sidebar, main panel, command palette |
| `src/screens` | One folder per screen |
| `src/components` | Shared components; `ui` holds the shadcn/ui-based primitives |
| `src/assets` | The product mark, also the source of the app icons in `src-tauri/icons` |
| `src-tauri` | The Rust side: the window, and later the CLI supervisor |

## Develop

You need Node 24 or later (`.node-version` at the repository root names it for version managers), pnpm 12 and Rust. On Linux, Tauri also needs the WebKitGTK development packages ([Tauri prerequisites](https://v2.tauri.app/start/prerequisites/)).

```sh
pnpm install     # from the repository root
cd apps/desktop
pnpm tauri dev   # the app in its window
pnpm dev         # the frontend alone, in a browser at http://localhost:1420
```

| Command | What it does |
| --- | --- |
| `pnpm test` | Runs the tests, including the checks on `tokens.css` and on the lint |
| `pnpm typecheck` | Type-checks the frontend |
| `pnpm lint` | Runs oxlint with Ultracite's rules, type-aware rules, the TypeScript type check and the design-system lint |
| `pnpm format` | Formats every file with oxfmt |
| `pnpm format-check` | Checks that every file is formatted, without changing any |
| `pnpm fix` | Applies the lint's safe fixes, then formats (`ultracite fix`) |
| `pnpm build` | Builds the frontend into `dist` |

The app is a package of the pnpm workspace at the repository root: `pnpm-workspace.yaml` lists the packages and holds the pnpm settings, and `pnpm-lock.yaml` sits next to it. Dependencies are pinned to exact versions, and `pnpm add` saves them that way. pnpm installs a version only once it has been published for a day, and runs install scripts only for the packages `allowBuilds` lists.

From the repository root, `make check-desktop` runs what CI runs for the desktop app: the frontend checks, then cargo fmt and Clippy for the Rust side.

## Rules for components

These rules are for anyone writing a component, people and coding agents alike. If a value is not a decision recorded in `DESIGN.md`, the code should not pass `pnpm lint` or `pnpm test`.

- Style with the semantic tokens through their Tailwind names: colours (`bg-shell-content-bg`), radii (`rounded-card`), shadows (`shadow-overlay`), layout tokens (`w-sidebar`, `pt-page-top px-page pb-page`; `DESIGN.md` §4 lists the class for each) and type roles (`type-page-title`, then a colour such as `text-text-primary`).
- The tokens live in `src/styles/tokens.css`, edited by hand. `DESIGN.md` documents them. When the design needs a value no token holds, add the token to `tokens.css` and to its table in `DESIGN.md` in the same change, then use it. Do not reuse another element's token because the value matches.
- The Tailwind theme holds only our tokens. `tokens.css` resets the default theme (`--*: initial`), so Tailwind's default colours, spacing, radii, shadows, fonts, font sizes, line heights, tracking and breakpoints do not exist. A class that names one produces no CSS, and the lint reports it as unknown.
- Spacing steps go from `0` to `12` in half steps, a 2px grid (`gap-2.5`, `h-9.5`). A larger or off-grid value needs a layout token.
- No escape hatches. The lint rejects arbitrary values (`text-[13px]`, `bg-[#fff]`), raw palette colours, unknown classes, inline styles and class strings it cannot read. A class never reads a CSS variable (`w-(--sidebar)`): every value is a theme token with its own class. The lint does not catch this one, so check for it in review.
- The lint does not catch a few utilities that need no theme value: alpha modifiers on colours (`bg-card-bg/50`), `rounded-full` and `leading-none`. Do not use them; add a token instead.
- Our components (anything imported from `src`) take no `className` and no `style`. Every visual choice is a closed, typed prop: `Button`'s `variant` and `size`, `Icon`'s `size`, `strokeWidth` and `tone`, `AgentxMark`'s `size`, which also sets its radius. The types reject anything else, and the lint reports a `className` or `style` on them. To place a component, wrap it in an element or use `gap` on the parent.
- When no prop fits, add a variant to the component with `cva()`, not a prop that takes free values.
- Screens and the app shell build on the components in `src/components`: native `<button>`, `<input>`, `<textarea>` and `<select>` are allowed only there. Use `Button` and its variants; add a component, or a variant to an existing one, when none fits.
- The lint reads classes in `className`, `cn()`, `cva()` and same-file variables. Keep class strings in one of these; a class string anywhere else (such as a library's `classNames` option) goes through `cn()` so it is checked too.
- Code generated with the shadcn CLI uses shadcn's default classes; map them to tokens and remove its `className` prop before committing, or the lint fails.
- Import from `src` with the `#/` prefix and the file's extension: `import { Icon } from "#/components/icon.tsx"`. `#/` is a Node subpath import declared in `package.json` (`"imports"`), so TypeScript, Vite and the lint resolve it the same way, and like Node they need the extension.
- Follow the copy rules in `DESIGN.md` §9 and the vocabulary in [`GLOSSARY.md`](../../GLOSSARY.md).
- Show only what the CLI reports. A screen never predicts the result of a command.
