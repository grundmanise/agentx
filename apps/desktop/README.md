# agentx desktop

The desktop app: a Tauri 2 window with a React and TypeScript frontend. Today it is the app shell with placeholder screens, the startup checks, and the CLI supervisor and transport the screens will use. By design it will only render what the `agentx` CLI reports and run CLI commands for every action; it never reads the machine, runs git or opens a network connection itself. See [ADR 0001](../../docs/adr/0001-go-cli-owns-machine-and-account-tauri-renders.md).

## What is here

| Path | What it holds |
| --- | --- |
| [`DESIGN.md`](DESIGN.md) | The design system: colour, type, spacing and radius tokens, components, motion and copy rules |
| `src/styles/tokens.css` | The tokens as Tailwind theme values and `type-*` utilities, edited by hand. It also resets Tailwind's default theme, so only these tokens exist |
| `src/styles/index.css` | The CSS entry: Tailwind, the fonts, the tokens and the base styles |
| `src/app` | The app shell: title bar, sidebar, main panel, command palette, and the startup layer |
| `src/startup` | The launch sequence: it resolves the CLI and runs `version`, `doctor` and `serve` before any screen mounts, and again when a command exits with code 8. It runs out of sight; the startup layer shows only when a check fails, and in manual mode. A launch that runs over 2 seconds says what it checks in the empty panel, and from 5 seconds into a check, how much longer that check keeps trying (`src/app/launch-progress.tsx`). `checks.ts` reads the five checks and the report from the CLI's output |
| `src/store` | The app's data from the CLI, in one zustand store (`store.ts`). `feed.ts` applies the serve child's events by `docs/spec/inventory-ordering.md`, adds what serve notices (drift, reconcile and update events, and the warnings it logs) to the activity list, and asks serve to scan again. `runner.ts` is the one way a control runs a command: it shows the command as pending, reports the outcome by exit code as a toast and an activity entry, and asks for a refresh after any command that may change something. `loaders.ts` fills settings, sources and the adoption preview from one-shot reads |
| `src/screens` | The screens; today one placeholder |
| `src/components` | Shared components; `ui` holds the shadcn/ui-based primitives |
| `src/lib` | Small helpers: `cn()` in `utils.ts`, and `navigate.ts`, which lets code outside React change the route |
| `src/cli/transport.ts` | The one way to the CLI: one-shot runs whose events arrive as they are written, cancel, and the serve child's events, log lines, exits and stdin requests |
| `src/cli/events.gen.ts`, `src/cli/commands.gen.ts` | The types of the CLI's events and exit codes, and the commands and flags the CLI takes, generated from the CLI's own code. Do not edit them: after changing an event or a command in `apps/cli`, run `go test ./internal/cli -run 'TestEventTypes\|TestCommandTypes' -update` there. Those tests fail while a file is stale |
| `src/assets` | Illustrations; today the product mark, also the source of the app icons in `src-tauri/icons` |
| `e2e` | The end-to-end tests: [Playwright](https://playwright.dev) drives the frontend in Chromium on the Vite dev server. `e2e/harness/cli.ts` is the mocked CLI they run against |
| `src/screens/skills` | The Skills screen. `projection.ts` turns the inventory and the store's slices into rows and groups, pure functions every later change reads a skill through. `skills-screen.tsx` holds the selection (the route, so `open.ts` can select a skill from any screen) and the keyboard; `skill-list.tsx`, `skill-row.tsx` and `filter-popover.tsx` are the left column |
| `src/assets/logos` | The agent client logos, one SVG per client slug, shown by `src/components/agent-logo.tsx`; a client with none gets a monogram. The app never loads a logo from the network |
| `e2e/fixtures` | Inline inventories and events shared by several tests, typed by the generated event types |
| `oxlint.config.ts`, `oxfmt.config.ts` | The lint and the formatter: [Ultracite](https://www.ultracite.ai)'s presets for oxlint and oxfmt, plus the design-system lint built on [`@shadcn/lint`](https://github.com/shadcn-ui/lint) and one rule of [`eslint-plugin-better-tailwindcss`](https://github.com/schoero/eslint-plugin-better-tailwindcss) |
| `src-tauri` | The Rust side: the window and the CLI supervisor, which reads the login shell's environment, finds the CLI, runs one-shot commands and keeps the serve child open |

## Develop

You need:

- Node 24 or later. `.node-version` at the repository root names it for version managers.
- pnpm 12. The root `package.json` pins the exact version in `packageManager`.
- Rust, and on Linux the WebKitGTK development packages ([Tauri prerequisites](https://v2.tauri.app/start/prerequisites/)).

```sh
pnpm install                            # from the repository root
cd apps/desktop
pnpm exec playwright install chromium   # once, for the end-to-end tests
pnpm tauri dev                          # the app in its window
pnpm dev                                # the frontend alone, at http://localhost:1420
```

A release build runs only the `agentx` next to its executable: the CLI of its own release, built from the same commit. A debug build (`pnpm tauri dev`) also looks on the `PATH` your login shell sets, so put a CLI there, for example with `go build -o ~/go/bin/agentx .` in `apps/cli`.

### How a release build gets its CLI

The Release workflow ([`.github/workflows/release.yml`](../../.github/workflows/release.yml)) builds the app once per platform, in the `desktop` job for macOS and the `desktop-linux` job for Linux, after the `build` job has built the CLI. Each job downloads the CLI build of its platform, unpacks it as `dist/agentx-<target triple>` at the repository root, and runs `tauri build` with a `--config` that sets the release version and `bundle.externalBin` to that file. Tauri treats the file as a sidecar: it copies it next to the app's executable, as `agentx`, and on macOS signs it with the app.

| Platform | The app's executable | The CLI next to it |
| --- | --- | --- |
| macOS | `agentx.app/Contents/MacOS/agentx-desktop` | `agentx.app/Contents/MacOS/agentx` |
| Linux `.deb` | `/usr/bin/agentx-desktop` | `/usr/bin/agentx`, on `PATH` |
| Linux AppImage | `usr/bin/agentx-desktop` inside the image | `usr/bin/agentx` inside the image |

`tauri.conf.json` keeps version `0.0.0` and lists no `externalBin`, so `pnpm tauri dev` needs no sidecar and nothing in the repository changes per release. To build a release-like app yourself, put a CLI at `dist/agentx-<target triple>` and pass the same config:

```sh
# from the repository root; the target triple is `rustc -vV | sed -n 's/^host: //p'`
go build -C apps/cli -o "../../dist/agentx-$(rustc -vV | sed -n 's/^host: //p')" .
cd apps/desktop
pnpm tauri build --config "{\"bundle\":{\"externalBin\":[\"$PWD/../../dist/agentx\"]}}"
```

The workflow checks, before it uploads, that the CLI inside each package reports the release version, and on macOS that the app passes `codesign --verify`, and `spctl` and `stapler validate` when it was notarized. [`RELEASING.md`](../../RELEASING.md) describes the release and its secrets.

The serve child's stderr goes to `~/Library/Logs/agentx/serve.log` on macOS and `$XDG_STATE_HOME/agentx/logs/serve.log` (`~/.local/state/agentx/logs` by default) on Linux, in at most 5 files of 5 MB. Its warnings also reach the app, which lists them in the activity list; its debug lines are only in the log.

Run these in `apps/desktop`:

| Command | What it does |
| --- | --- |
| `pnpm check` | Checks without changing files (`ultracite check`): oxlint with Ultracite's rules, the type-aware rules, a type check and the design-system lint, plus the oxfmt format check. The lint covers the generated files in `src/cli`, which Ultracite skips by default, so the type check covers them too |
| `pnpm fix` | Applies the lint's safe fixes, formats, then lists what is left (`ultracite fix`) |
| `pnpm test` | Runs the end-to-end tests in Chromium. It starts the dev server, or reuses a running one outside CI |
| `pnpm build` | Builds the frontend into `dist` with Vite. It does not check types: `pnpm check` does, and `pnpm tauri build` runs `pnpm check` first |

From the repository root, `make check-desktop` runs what CI runs for the desktop app: `pnpm install --frozen-lockfile`, then `check`, `test` and `build`, then `cargo fmt --check`, Clippy and `cargo test` for the Rust side. `cargo test` drives the supervisor against a stub CLI script.

### The pnpm workspace

The app is a package of the pnpm workspace at the repository root. `pnpm-workspace.yaml` lists the packages (`apps/desktop` and `packages/*`) and holds the pnpm settings, and `pnpm-lock.yaml` sits next to it.

- Dependencies are pinned to exact versions, and `pnpm add` saves them that way.
- pnpm installs a version only once it has been published for a day.
- Install scripts run only for the packages `allowBuilds` lists. Today it lists none.

### Tests

- The tests in `e2e` find elements by role and accessible name, as a person using a screen reader would, never by class.
- Code the app does not reach yet, such as a component on its own or a `notify()` call, runs from a module in `e2e/harness`. A test loads it into the dev server's page with `page.addScriptTag`; the app never imports it.
- To use a Chromium already on your machine instead of Playwright's download, set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` to its path.
- The dev server has no Tauri runtime. A test that imports `test` from `e2e/harness/cli.ts` runs against the mocked CLI: the app's own `transport` module talks to it as it would to the Rust supervisor. Every test that loads the app imports it, since the app checks the machine on launch: the mock starts with a passing launch (`version` and `doctor` replies, and a serve child that writes its first inventory), which a test replaces with `register()` and `onServeStart()`. The test registers a reply per command line (a few events, typed by `events.gen.ts` with only the fields it checks, and an exit code), can hold a run open to see a pending state or a cancel, plays serve events, log lines and exits, and reads back every command line, cancel and serve request.
- The mock refuses a command line the CLI would refuse, by `commands.gen.ts`: an unknown command or flag, a flag that takes a value given none, or a boolean flag given one. A refused line, or one with no registered reply, fails the test.

## Rules for components

These rules are for anyone writing a component, people and coding agents alike.

### Tokens

- Style with the semantic tokens through their Tailwind names: colours (`bg-shell-content-bg`), radii (`rounded-card`), shadows (`shadow-overlay`), layout tokens (`w-sidebar`, `pt-page-top px-page pb-page`; `DESIGN.md` §4 lists the class for each) and type roles (`type-page-title`, then a colour such as `text-text-primary`).
- The tokens live in `src/styles/tokens.css`, edited by hand, and `DESIGN.md` documents them. When the design needs a value no token holds, add the token to `tokens.css` and to its table in `DESIGN.md` in the same change, then use it. Do not reuse another element's token because the value matches.
- The Tailwind theme holds only our tokens. `tokens.css` resets the default theme (`--*: initial`), so Tailwind's default colours, spacing, radii, shadows, fonts, font sizes, line heights, tracking and breakpoints do not exist.
- Spacing steps go from `0` to `12` in half steps, a 2px grid (`gap-2.5`, `h-9.5`). A larger or off-grid value needs a layout token.
- A class never reads a CSS variable (`w-(--sidebar)`): every value is a theme token with its own class.

### Components

- Our components (anything imported from `src`) take no `className` and no `style`. Every visual choice is a closed, typed prop: `Button`'s `variant` and `size`, an icon's `size` (`DESIGN.md` §7 lists the sizes), `AgentxMark`'s `size`, which also sets its radius. To place a component, wrap it in an element or use `gap` on the parent.
- When no prop fits, add a variant to the component with `cva()`, not a prop that takes free values.
- Native `<button>`, `<input>`, `<textarea>` and `<select>` are allowed only in `src/components`. Screens and the app shell use `Button` and the other components there; add a component, or a variant, when none fits.
- Code generated with the shadcn CLI uses shadcn's default classes; map them to tokens and remove its `className` prop before committing.
- Import from `src` with the `#/` prefix and the file's extension: `import { SearchIcon } from "#/components/icons/search.tsx"`. `#/` is a Node subpath import declared in `package.json` (`"imports"`), so TypeScript, Vite and the lint resolve it the same way, and like Node they need the extension.
- Follow the copy rules in `DESIGN.md` §9 and the vocabulary in [`GLOSSARY.md`](../../GLOSSARY.md).
- Show only what the CLI reports. A screen never predicts the result of a command.
- Read CLI data from `useAppStore` (`src/store/store.ts`) with a selector, and run a command with `runCommand` (`src/store/runner.ts`), never with the transport directly. The screen redraws from the inventory that follows the command.

### What the checks catch

`pnpm check` rejects:

- a `className` or `style` on our components, at every call site
- a value outside a closed prop, such as `variant="link"` on `Button` or `size={16}` on an icon (the type check)
- arbitrary values (`text-[13px]`, `bg-[#fff]`), a class that reads a CSS variable (`w-(--sidebar)`), raw palette colours (`bg-red-500`) and classes Tailwind does not know (`p-13`)
- inline styles, and native controls outside `src/components`
- class strings the lint cannot read. It reads classes in `className`, `cn()`, `cva()` and same-file variables, so a class string anywhere else (such as a library's `classNames` option) goes through `cn()`.

It misses these, so check for them in review:

- alpha modifiers on colours (`bg-card-bg/50`), `rounded-full` and `leading-none`, which need no theme value
- a token reused for an element it was not made for
