# agentx desktop: design system

The visual contract for the desktop app. Code uses the semantic tokens below, never a primitive or a raw value. The tokens live in `src/styles/tokens.css`, which is edited by hand and is the source of truth; this file documents them. When you add or change a token, edit `tokens.css` and the matching table here in the same change. The tables use dotted names, and `tokens.css` names them like this:

- A colour token `shell.bg` is `--color-shell-bg`, a Tailwind colour (`bg-shell-bg`, `text-text-muted`). Primitives are `--gray-990` and so on, outside the Tailwind theme, so no utility class can name one.
- A tone `tone.primary` is `--color-tone-primary-fg`, `-bg` and `-border`.
- A shadow token `overlay.shadow` is `--shadow-overlay` (`shadow-overlay`).
- A radius token `card.tile-radius` is `--radius-card-tile` (`rounded-card-tile`).
- A layout token is a Tailwind theme value in the namespace of the utility that uses it, named after the element: `shell.sidebar-width` is `--width-sidebar` (`w-sidebar`), `page.padding` is `--padding-page` and `--padding-page-top` (`pt-page-top px-page pb-page`). §4 lists the class for each token. A token given as a range (`38–40`) is guidance, not a variable; pick the value the component calls for.
- A type token `type.page-title` is the utility `type-page-title`, which sets the face, size, weight, line height, tracking and case together.

The Tailwind theme holds only these tokens. `tokens.css` resets Tailwind's whole default theme (`--*: initial`), so no default colour, spacing step, radius, shadow, font, font size, weight, line height, tracking, breakpoint or animation exists. Besides the tokens above, the theme has three font weights (`font-normal`, `font-medium`, `font-semibold`) and the spacing steps of §4: a 2px grid in half steps from `0` to `12` (48px), with no base `--spacing`, so `p-13` or `top-28` produce no CSS. `pnpm check` rejects any class Tailwind does not know and any arbitrary value (`text-[13px]`). When a design needs a value no token holds, add the token to `tokens.css` and to the table here; see the README's rules for components.

## 1. Character

A dark, quiet desktop tool. One accent (lime) carries the accent and action signals; everything else is near-black and grey. Serif for page titles only; a mono face for anything technical (paths, ids, dates, captions). Surfaces are flat: elevation comes from one shade step and, on floating layers, a soft black shadow. No gradients, no emoji, no icon-per-row decoration.

Copy voice: short, plain, present tense.

## 2. Color

Two layers. **Primitives** are the raw palette and never appear in component code. **Semantic tokens** name a role and point at one primitive; everything in §3–§8 and in the app's code uses semantic names only. In `tokens.css` a semantic token is a Tailwind theme colour that points at a primitive (`--color-shell-bg: var(--gray-990)`); the names below are the contract.

### 2.1 Primitives

Grey ramp (dark → light):

| Name       | Hex       |
| ---------- | --------- |
| `gray-990` | `#0b0b0c` |
| `gray-980` | `#0e0e10` |
| `gray-970` | `#121214` |
| `gray-960` | `#131315` |
| `gray-950` | `#141416` |
| `gray-940` | `#151517` |
| `gray-930` | `#17171a` |
| `gray-920` | `#18181b` |
| `gray-910` | `#1a1a1d` |
| `gray-900` | `#1c1c20` |
| `gray-890` | `#1d1d21` |
| `gray-880` | `#1f1f23` |
| `gray-870` | `#222226` |
| `gray-860` | `#232327` |
| `gray-850` | `#26262b` |
| `gray-800` | `#2c2c31` |
| `gray-700` | `#3a3a41` |
| `gray-600` | `#4a4a52` |
| `gray-500` | `#80808b` |
| `gray-400` | `#86868e` |
| `gray-300` | `#8b8b93` |
| `gray-250` | `#a0a0a8` |
| `gray-200` | `#b4b4bb` |
| `gray-150` | `#c9c9cf` |
| `gray-100` | `#dcdce0` |
| `gray-50`  | `#ededef` |

Hues:

| Name         | Hex       |
| ------------ | --------- |
| `lime-400`   | `#d4ff4f` |
| `lime-300`   | `#e2ff7a` |
| `amber-400`  | `#ffc15e` |
| `red-400`    | `#ff7b72` |
| `violet-400` | `#b69cff` |
| `blue-400`   | `#7cb7ff` |
| `white`      | `#ffffff` |
| `black`      | `#000000` |

Alpha is written as `<primitive>/<percent>` and resolves to `rgba` of that primitive (e.g. `lime-400/12` → `rgba(212,255,79,.12)`, `black/55` → `rgba(0,0,0,.55)`).

### 2.2 Semantic tokens

Grouped by element. Each token points at one primitive (or at a meaning token when the value is shared by design). §4–§6 and the app's code use these names only; two elements with a look-alike value still get their own token so one can change without the other.

#### Meaning (accents and tones)

| Token | Primitive | Meaning |
| --- | --- | --- |
| `accent.primary` | `lime-400` | Primary action, enabled, update candidate found, success, selection |
| `accent.primary-hover` | `lime-300` | Primary button hover |
| `accent.attention` | `amber-400` | Edited, displaced, needs a look but not broken |
| `accent.danger` | `red-400` | Removed, unreachable, destructive |
| `accent.yours` | `violet-400` | Your own skills, "Yours" side of a diff, publish |
| `accent.theirs` | `blue-400` | "Base version / Update" side of a diff |
| `on-accent` | `gray-990` | Text and icons on any accent or inverse fill |

Tone triples `{ fg, bg, border }` for banners and Inbox icon tiles:

| Tone             | fg                 | bg             | border          |
| ---------------- | ------------------ | -------------- | --------------- |
| `tone.primary`   | `accent.primary`   | `lime-400/8`   | `lime-400/25`   |
| `tone.attention` | `accent.attention` | `amber-400/8`  | `amber-400/25`  |
| `tone.danger`    | `accent.danger`    | `red-400/8`    | `red-400/25`    |
| `tone.yours`     | `accent.yours`     | `violet-400/8` | `violet-400/25` |
| `tone.neutral`   | `gray-300`         | `gray-910`     | `gray-800`      |

#### Text (shared across elements)

| Token | Primitive | Element |
| --- | --- | --- |
| `text.primary` | `gray-50` | Titles, body, values |
| `text.secondary` | `gray-200` | Secondary text, Details values |
| `text.body` | `gray-150` | Card copy, palette rows |
| `text.muted` | `gray-300` | Descriptions, banner text |
| `text.hint` | `gray-400` | Helper copy, Details keys, "+n" counts |
| `text.caption` | `gray-500` | Uppercase mono captions, chevrons, line counts |
| `text.disabled` | `gray-600` | Disabled text and glyphs, inactive dots |
| `text.faint` | `gray-700` | Line numbers, hollow timeline ring |

#### Window and shell

| Token | Primitive | Element |
| --- | --- | --- |
| `shell.bg` | `gray-990` | Window body, sidebar, startup layer |
| `shell.content-bg` | `gray-970` | Content area, both split columns, command field |
| `shell.divider` | `gray-890` | Split column divider, section dividers, table header rule |
| `shell.scrim` | `black/50` | Behind the history sheet |
| `shell.scrim-strong` | `black/55` | Behind dialogs and the palette |
| `shell.nav-text` | `gray-300` | Inactive nav item; `text.primary` on hover |
| `shell.nav-active-bg` | `gray-910` | Active nav item |
| `shell.nav-active-text` | `gray-50` | Active nav item label |
| `shell.nav-badge-bg` | `accent.primary` | Inbox count badge (digits `on-accent`) |
| `shell.topbar-hover` | `gray-920` | Account remote status hover (the command field changes only its border and text on hover) |
| `shell.topbar-text` | `gray-300` | Command field placeholder and icon (`text.secondary` on hover), account remote status label |
| `shell.status-published` | `accent.primary` | Account remote status dot, nothing to publish (halo `lime-400/15`) |
| `shell.status-pending` | `accent.yours` | Account remote status dot, versions to publish (halo `violet-400/18`) |
| `shell.logo-mark` | `accent.primary` | The X in the product mark |
| `shell.logo-bg` | `gray-990` | Product mark tile |
| `shell.logo-ring` | `gray-860` | 1px ring around the product mark |
| `shell.focus-ring` | `accent.primary` | Keyboard focus outline on any element |
| `shell.selection-bg` | `lime-400/30` | Selected text |

#### Buttons

| Token | Primitive | Element |
| --- | --- | --- |
| `button.primary-bg` | `accent.primary` | Primary button |
| `button.primary-hover` | `accent.primary-hover` |  |
| `button.primary-text` | `on-accent` |  |
| `button.secondary-bg` | `gray-880` | Secondary button, Reset button |
| `button.secondary-hover` | `gray-850` |  |
| `button.secondary-text` | `gray-50` |  |
| `button.ghost-text` | `gray-200` | Ghost button label (or `text.muted` in quiet spots) |
| `button.ghost-hover` | `gray-880` | Ghost / icon button hover |
| `button.danger-text` | `accent.danger` | Danger ghost (Remove, Cancel, Restore base version) |
| `button.danger-hover` | `red-400/10` |  |
| `source-pill.bg` | `gray-910` | Source pill in Add, inactive (label `text.secondary`) |
| `source-pill.active-bg` | `gray-50` | Source pill in Add, active |
| `source-pill.active-text` | `on-accent` |  |
| `button.toast-action-text` | `accent.primary` | Action inside a toast |
| `button.split-divider` | `gray-800` | The 1px line between the two halves of a split button |

#### Status pills

| Token | Primitive | Element |
| --- | --- | --- |
| `pill.update-bg` | `accent.primary` | "Update" |
| `pill.update-text` | `on-accent` |  |
| `pill.neutral-bg` | `gray-860` | "Not published", "Adopt", "Reconnect" |
| `pill.neutral-hover` | `gray-800` |  |
| `pill.neutral-text` | `gray-200` |  |
| `pill.removed-bg` | `red-400/12` | "Removed from source" |
| `pill.removed-text` | `accent.danger` |  |
| `pill.disabled-bg` | `amber-400/10` | Plugin "Disabled" |
| `pill.disabled-text` | `accent.attention` |  |
| `pill.version-bg` | `gray-880` | Plugin version |
| `pill.version-text` | `gray-300` |  |

#### Inputs, switches, checkboxes

| Token                 | Primitive        | Element                           |
| --------------------- | ---------------- | --------------------------------- |
| `input.bg`            | `gray-980`       | Text inputs, search fields        |
| `input.border`        | `gray-860`       |                                   |
| `input.border-focus`  | `gray-700`       |                                   |
| `input.text`          | `gray-50`        |                                   |
| `input.placeholder`   | `gray-500`       | Placeholder text and leading icon |
| `switch.on-track`     | `accent.primary` | Switch on (full and compact)      |
| `switch.on-knob`      | `on-accent`      |                                   |
| `switch.off-track`    | `gray-850`       | Switch off                        |
| `switch.off-knob`     | `gray-400`       |                                   |
| `checkbox.ring`       | `gray-700`       | Unchecked popover checkbox        |
| `checkbox.checked-bg` | `accent.primary` |                                   |
| `checkbox.check`      | `on-accent`      |                                   |

#### Segmented control (Update review, filter State)

| Token | Primitive | Element |
| --- | --- | --- |
| `segmented.track-bg` | `gray-950` | Track (`gray-980` when it sits on a card) |
| `segmented.track-border` | `gray-870` |  |
| `segmented.text` | `gray-50` | Unselected segment; `gray-400` once another segment is chosen |
| `segmented.yours-selected` | `accent.yours` | "Keep yours" (label `on-accent`) |
| `segmented.theirs-selected` | `accent.theirs` | "Use base version / update" |
| `segmented.both-selected` | `gray-50` | "Keep both" |
| `segmented.neutral-selected` | `gray-850` | Filter State segments (label `text.primary`) |

#### Filter row (Skills toolbar)

| Token | Primitive | Element |
| --- | --- | --- |
| `filter.bg` | `gray-880` | Filter button at rest |
| `filter.text` | `gray-300` |  |
| `filter.active-bg` | `lime-400/12` | Filter button with any filter applied |
| `filter.active-text` | `accent.primary` |  |
| `filter.badge-bg` | `accent.primary` | Count badge (digits `on-accent`) |
| `filter.chip-bg` | `gray-920` | Applied agent client chip |
| `filter.chip-text` | `gray-150` |  |
| `filter.chip-hover` | `gray-880` |  |
| `filter.sort-text` | `gray-300` | A–Z toggle |

#### Lists and rows

| Token | Primitive | Element |
| --- | --- | --- |
| `row.hover` | `gray-920` | Skills row hover |
| `row.selected-bg` | `gray-900` | Selected Skills row |
| `row.selected-bar` | `accent.primary` | 2px bar on the selected row |
| `row.name` | `gray-50` | Row name, enabled |
| `row.name-off` | `gray-300` | Row name when in no agent client |
| `row.desc` | `gray-300` | Description line |
| `row.chip-ring` | `shell.content-bg` | 2px ring behind overlapping agent client logos in rows |
| `group.label` | `gray-300` | Source group caption |
| `group.count` | `gray-500` | Count beside the caption |
| `group.divider-label` | `gray-500` | "NOT IN ANY AGENT CLIENT" sub-divider |
| `table.row-hover` | `gray-930` | Hover on Inbox update rows, server rows, Settings sources |
| `table.header-text` | `gray-500` | Column captions |

#### Skill glyph

| Token | Primitive | Element |
| --- | --- | --- |
| `glyph.enabled` | `accent.primary` | Dot or fork glyph when enabled (and, for the fork glyph, published) |
| `glyph.off` | `gray-600` | Glyph when in no agent client or not published |
| `glyph.edited` | `accent.attention` | Pencil glyph |

#### Agent client logos and toggle chips

| Token | Primitive | Element |
| --- | --- | --- |
| `logo.bg` | `white` | Round agent client logo |
| `logo.ring` | = parent bg | 2px ring between overlapping logos (`shell.content-bg`, `overlay.bg`, `card.bg`…) |
| `agent-chip.off-bg` | `gray-950` | Toggle chip, off |
| `agent-chip.off-border` | `gray-870` |  |
| `agent-chip.on-bg` | `lime-400/6` | Toggle chip, on |
| `agent-chip.on-border` | `lime-400/35` |  |
| `agent-chip.hover-border` | `gray-700` |  |
| `agent-chip.text` | `gray-50` | Agent client name |

#### Cards and banners

| Token | Primitive | Element |
| --- | --- | --- |
| `card.bg` | `gray-950` | Inbox card, agent client card, plugin card, file-tree card, expanded review file card |
| `card.border` | `gray-870` |  |
| `card.title` | `gray-50` |  |
| `card.text` | `gray-300` |  |
| `card.tag` | `gray-500` | Mono tag beside a card title |
| `card.subrow-hover` | `gray-930` | Sub-rows inside the Updates card |
| `add-card.bg` | `gray-930` | Add skills card |
| `add-card.border` | `gray-870` |  |
| `add-card.hover-border` | `gray-700` |  |
| `add-card.selected-bg` | `lime-400/6` |  |
| `add-card.selected-border` | `lime-400/50` |  |
| `add-card.check-bg` | `accent.primary` | Check tile on a selected card (check `on-accent`) |
| `add-card.status-added` | `gray-400` | "Added" label |
| `add-card.status-forked` | `accent.yours` | "Forked" label |
| `add-card.status-disk` | `accent.attention` | "Already in library" label |
| `add-card.status-in-use` | `gray-400` | "Name in use" label |
| `add-card.check-ring` | `gray-700` | Ring of the round check on a card that is not selected |
| `banner.title` | `gray-50` | Banner title (bg/border/tile from `tone.*`) |
| `banner.text` | `gray-300` |  |
| `card.chip-ring` | `card.bg` | 2px ring behind overlapping agent client logos on a card (the Universal card) |
| `plugin.skills-dot` | `accent.yours` | SKILLS group (tint `violet-400/10`) |
| `plugin.servers-dot` | `accent.theirs` | SERVERS group (tint `blue-400/10`) |
| `plugin.hooks-dot` | `accent.attention` | HOOKS group (tint `amber-400/10`) |
| `plugin.connector` | `gray-870` | Nested-content connector lines |
| `plugin.row-hover` | `gray-930` | Hover on a plugin card's skill and server rows |

#### Overlays (dialog, popover, palette, sheet, toast, floating bar)

| Token | Primitive | Element |
| --- | --- | --- |
| `overlay.bg` | `gray-940` | Dialog, popover, palette, toast, floating bar |
| `overlay.border` | `gray-800` |  |
| `overlay.shadow` | `0 40px 100px -20px black/80` | Dialog, palette, sheet |
| `popover.shadow` | `0 24px 60px -12px black/70` |  |
| `popover.row-hover` | `gray-880` |  |
| `popover.caption` | `gray-500` | SHOW / STATE / AGENTS |
| `menu.row-hover` | `gray-880` | Menu row pointed at or focused |
| `menu.shadow` | `0 24px 60px -12px black/70` | Menu |
| `palette.row-text` | `gray-150` |  |
| `palette.row-active` | `gray-870` |  |
| `palette.icon` | `gray-400` | Search icon and row icons |
| `palette.icon-active` | `accent.primary` | Row icon in the selected row |
| `dialog.title` | `gray-50` |  |
| `dialog.subtitle` | `gray-400` |  |
| `sheet.bg` | `gray-960` | History sheet body |
| `sheet.border` | `gray-850` |  |
| `toast.text` | `gray-50` |  |

#### Code, file tree and diffs

| Token | Primitive | Element |
| --- | --- | --- |
| `code.bg` | `gray-980` | Code pane, hunk block |
| `code.border` | `gray-890` | Hunk block border |
| `code.line-number` | `gray-400` | Line numbers: 5.3:1 on `code.bg`, 4:1 on the added tint, where the marker carries the meaning |
| `code.frontmatter` | `gray-400` | Front-matter and keys |
| `code.heading` | `gray-50` | Markdown headings |
| `code.list` | `gray-150` | List items |
| `code.prose` | `gray-250` | Prose |
| `code.strong` | `gray-100` | Code in diff panes |
| `code.context` | `gray-500` | Context lines around a change |
| `file-row.hover` | `gray-920` | File tree row hover |
| `file-row.divider` | `gray-890` |  |
| `file-row.path` | `gray-200` | Path |
| `file-row.path-open` | `gray-50` | Path of the file whose pane is open |
| `file-row.meta` | `gray-500` | "{n} lines" |
| `file-row.edited-label` | `accent.attention` | "edited by you" / "added by you" |
| `diff.added-bg` | `lime-400/12` | Added line tint (marker "+" `accent.primary`) |
| `diff.removed-bg` | `red-400/8` | Removed line tint (marker "−" `accent.danger`) |
| `diff.yours-label` | `accent.yours` | "Yours" label, dot and chosen-pane ring |
| `diff.yours-bg` | `violet-400/6` | "Yours" pane ground |
| `diff.theirs-label` | `accent.theirs` | "Base version / Update" label, dot and chosen-pane ring |
| `diff.theirs-bg` | `blue-400/6` | "Base version / Update" pane ground |
| `diff.file-pending` | `accent.attention` | File dot while changes remain |
| `diff.file-done` | `accent.primary` | File dot when every change is decided |
| `diff.progress-done` | `accent.primary` | Progress segment, decided |
| `diff.progress-pending` | `gray-850` | Progress segment, pending |

#### Timeline (History)

| Token                  | Primitive        | Element                         |
| ---------------------- | ---------------- | ------------------------------- |
| `timeline.dot-install` | `gray-600`       | Added / found / created entries |
| `timeline.dot-update`  | `accent.primary` | Updated, adopted, put back      |
| `timeline.dot-yours`   | `accent.yours`   | Published, forked               |
| `timeline.connector`   | `gray-890`       | 1px line between dots           |
| `timeline.meta`        | `gray-500`       | Relative time under an entry    |
| `timeline.more-ring`   | `gray-700`       | Hollow "Show all" dot ring      |

#### Startup checks

| Token | Primitive | Element |
| --- | --- | --- |
| `check.pass-bg` | `lime-400/12` | ✓ mark tile |
| `check.pass-mark` | `accent.primary` |  |
| `check.fail-bg` | `red-400/12` | "!" mark tile |
| `check.fail-mark` | `accent.danger` |  |
| `check.wait-bg` | `gray-880` | Pending mark tile |
| `check.wait-mark` | `gray-500` |  |
| `check.label` | `gray-50` | Check title |
| `check.label-pending` | `gray-300` | Check title while waiting or running |
| `check.detail` | `gray-400` | Mono detail line |

#### MCP servers

| Token | Primitive | Element |
| --- | --- | --- |
| `server.transport-bg` | `gray-880` | Transport pill |
| `server.transport-text` | `gray-200` |  |
| `server.command` | `gray-500` | Command / URL under the name |
| `server.tools` | `gray-200` | "{n} tools" |
| `server.tools-auth` | `accent.attention` | "needs sign-in" |
| `server.tools-fail` | `accent.danger` | "unreachable", "timed out", "handshake failed" |
| `server.tools-none` | `gray-500` | "Not checked" |
| `server.versions` | `accent.attention` | "{n} versions" when agent configurations disagree |

### Contrast rules

Every text colour token reaches at least 4.5:1 (WCAG AA) on the backgrounds it is used on: the shell, page, card, sheet, overlay (palette, dialog, popover, toast), input and code backgrounds, the row and table hover fills, and the button and pill fills. The tightest pairs are `text.caption` (`gray-500`) on `row.hover` at 4.54:1 and `text.muted` (`gray-300`) on `palette.row-active` at 4.69:1.

- Body text is `text.primary`–`text.muted`. `text.hint` and `text.caption` are for helper copy, keys, captions, counts, meta and placeholders.
- `text.hint` and `text.caption` reach 4.5:1 on the surfaces and hover fills above, not on lighter fills. On `palette.row-active`, a button fill or a pill, use `text.muted` or lighter: a palette row's hint turns `text.muted` while the row is selected.
- `gray-500` is the darkest grey for text a person needs to read. `text.disabled` and `text.faint` (`gray-600`, `gray-700`) stay below 4.5:1, so use them only for disabled controls, which WCAG exempts, and for line numbers and marks that are not content.
- Check a new pairing before you use it, and keep it at 4.5:1 or more.
- Never put alpha-muted text on accent fills.

## 3. Typography

Fonts (bundled with the app from `@fontsource` packages; the app never loads anything from the network): **Instrument Serif** 400 (titles), **Geist** 400–700 (UI), **Geist Mono** 400–600 (technical).

Each row is a type token: a role with its face, size, weight, line height, tracking and case. A token with one value per column is a `type-*` utility in `tokens.css`; a row with a range (`11–12`) is guidance, not a utility – add a token with one value when a component needs it. Text colour is a separate colour token (§2.2). An empty cell inherits.

| Token | Face | Size | Weight | Line height | Tracking | Case | Element |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `type.page-title` | serif | 56 | 400 | 1 | -0.02em |  | Page title |
| `type.inbox-headline` | serif | 68 | 400 |  |  |  | Inbox headline; second clause italic `text.muted` |
| `type.review-title` | serif | 52 | 400 |  |  |  | Review title |
| `type.dialog-title` | serif | 36 | 400 |  |  |  | Dialog and sheet title |
| `type.startup-title` | serif | 48 | 400 | 1 | -0.02em |  | Startup title |
| `type.check-detail` | mono | 12.5 | 400 | 1.5 |  |  | Startup check detail (`check.detail`) |
| `type.check-hint` | sans | 13 | 400 | 1.5 |  |  | Startup check hint callout (`text.secondary`) |
| `type.skill-name` | sans | 30 | 600 |  | -0.035em |  | Detail pane skill name |
| `type.body` | sans | 14 | 400 |  |  |  | Body, palette rows |
| `type.input` | sans | 14 | 400 |  |  |  | Text inputs and search fields |
| `type.input-mono` | mono | 14 | 400 |  |  |  | Inputs that take a URL or a command |
| `type.add-card-name` | sans | 15 | 500 |  |  |  | Skill name on an Add skills card |
| `type.add-card-desc` | sans | 13.5 | 400 | 1.5 |  |  | Description on an Add skills card (`add-card.text`: `text.muted`) |
| `type.row-name` | sans | 14 | 500 |  |  |  | Row name |
| `type.row-desc` | sans | 12.5 | 400 |  |  |  | Row description (`text.muted`, single line, ellipsis) |
| `type.secondary` | sans | 13 | 400 |  |  |  | Secondary line (`text.hint`) |
| `type.button` | sans | 14 |  |  |  |  | Buttons; the variant sets the weight (600 primary, 500 others) |
| `type.button-sm` | sans | 13.5 |  |  |  |  | Small buttons |
| `type.pill` | sans | 11.5 | 600 |  |  |  | Pills, the letter of a monogram logo |
| `type.segment` | sans | 12.5 | 500 |  |  |  | Segmented control labels |
| `type.caption` | mono | 10.5 | 400 |  | 0.16em | upper | Captions (`text.caption`) |
| `type.technical` | mono | 12.5 | 400 |  |  |  | Paths, ids, dates, commands; a skill's origin in the detail header (`text.caption`) |
| `type.technical-sm` | mono | 12 | 400 |  |  |  | Details values (`text.secondary`), file status in the History sheet, the count beside a group header |
| `type.code` | mono | 12.5 | 400 | 1.75 |  |  | Code pane |
| `type.brand` | sans | 14 | 600 |  | -0.01em |  | Product name in the sidebar |
| `type.machine-label` | sans | 12 | 400 |  |  |  | Machine label under the product name (`text.muted`) |
| `type.nav` | sans | 14 | 500 |  |  |  | Nav item |
| `type.command-field` | sans | 13 | 400 |  |  |  | Command field placeholder |
| `type.key-hint` | mono | 11 | 400 |  |  |  | Key hint (`esc`, `⌘K`) |
| `type.palette-input` | sans | 17 | 400 |  |  |  | Palette search input |
| `type.palette-hint` | mono | 12 | 400 |  |  |  | Hint at the end of a palette row, keys in the palette footer |
| `type.palette-footer` | sans | 12 | 400 |  |  |  | Palette footer (`text.hint`) |
| `type.toast` | sans | 13.5 | 400 |  |  |  | Toast message |
| `type.toast-action` | sans | 13 | 600 |  |  |  | Action inside a toast |
| `type.detail-desc` | sans | 14.5 | 400 | 1.5 |  |  | Detail pane description (`text.muted`, `text-wrap:pretty`) |
| `type.banner-title` | sans | 14 | 500 |  |  |  | Banner title (`banner.title`); its text is `type.secondary` in `banner.text` |
| `type.agent-chip` | sans | 13 | 500 |  |  |  | Agent client name in a toggle chip |
| `type.agent-card-name` | sans | 16 | 600 |  |  |  | Agent client name on an Agents page card, and "Universal" on the Universal card |
| `type.timeline-entry` | sans | 13.5 | 400 | 20px |  |  | Timeline entry subject (`text.primary`); its line height is `timeline.line`, so the dot beside it sits on the first line |
| `type.timeline-meta` | sans | 12 | 400 |  |  |  | Timeline entry meta (`text.caption`) |
| `type.notice` | sans | 13 | 400 | 1.5 |  |  | Stale bar text; hint and warnings in an error toast |
| `type.server-name` | sans | 14.5 | 500 |  |  |  | Server name in a server row (`text.primary`) |
| `type.transport-pill` | mono | 11.5 | 500 |  |  |  | Transport pill (`server.transport-text` on `server.transport-bg`) |
| `type.settings-label` | sans | 14 | 500 |  |  |  | Settings section label (`text.primary`); the line under it is `type.secondary` in `text.muted` |

Text wrapping: `text-wrap:pretty` on multi-line copy. Single-line cells use `white-space:nowrap; overflow:hidden; text-overflow:ellipsis`.

## 4. Layout and spacing

Spacing is a semantic token per element, not a shared scale. Two tokens with the same step name (`button.gap`, `card.gap`) are independent values; change one without touching the other. Primitive px values are listed so the system can be audited, but code references the token.

Each token is a Tailwind theme value, and the Class column shows how code uses it. The theme variable sits in the namespace of its utility (`--width-*` for `w-*`, `--height-*` for `h-*`, `--padding-*`, `--margin-*`, `--gap-*`, `--inset-*` for `top-*` and the other sides, `--size-*`, `--max-width-*`, `--max-height-*`, `--min-height-*`, `--ring-width-*`, `--outline-width-*`, `--outline-offset-*`, `--grid-template-columns-*`) and is named after the element, so `w-command` and `h-command` are two tokens. A padding or margin with different sides is split into one theme value per value: the value most sides share takes the element's name (on a tie, the x value), and each other value adds its side (`-top`, `-right`, `-bottom`, `-left`, `-y`). A zero side uses the `0` step. Classes never read a CSS variable directly (`w-(--sidebar)`); the lint rejects it.

A size or gap inside a component that no token lists uses Tailwind's spacing steps in whole or half steps, a 2px grid (`gap-2.5` is 10px, `pb-5.5` is 22px). The steps stop at `12` (48px). Never a quarter step or an arbitrary value; a value off the grid or above 48px needs a layout token.

### Window and shell

| Token | px | Class | Element |
| --- | --- | --- | --- |
| `shell.titlebar-height` | 50 | `h-titlebar` | Title bar |
| `shell.sidebar-width` | 216 | `w-sidebar` | Sidebar |
| `shell.sidebar-padding` | 6 12 14 14 | `pt-sidebar-top pr-sidebar-right pb-sidebar pl-sidebar` | Sidebar inner padding |
| `shell.nav-height` | 36 | `h-nav` | Nav item |
| `shell.nav-gap` | 2 | `gap-nav` | Between nav items |
| `shell.nav-icon-gap` | 11 | `gap-nav-icon` | Icon → label in a nav item |
| `shell.command-width` | 460 | `w-command` | Command field |
| `shell.command-height` | 32 | `h-command` |  |
| `shell.status-height` | 30 | `h-status` | Account remote status pill |
| `shell.panel-inset` | 10 | `mr-panel mb-panel` | Main panel inset from the right and bottom |
| `stale-bar.height` | 36 | `h-stale-bar` | Stale bar at the top of the main panel |
| `stale-bar.padding` | 0 16 | `px-stale-bar` |  |
| `shell.focus-ring-width` | 2 | none: `index.css` sets it on every element | Keyboard focus outline |
| `shell.focus-ring-offset` | 2 | none: as above |  |

### Pages

| Token | px | Class | Element |
| --- | --- | --- | --- |
| `page.padding` | 40 48 48 | `pt-page-top px-page pb-page` | Content pages (Inbox, Agents, Servers, Plugins, Settings) |
| `page.title-gap` | 28 | `gap-page-title` | Title block → first section |
| `page.section-gap` | 28–40 |  | Between sections |
| `page.max-width` | 1200 | `max-w-page` | Inbox |
| `split.list-width` | 408 | `w-split-list` | Skills left column |
| `split.header-padding` | 30 20 0 28 | `pt-split-header-top pr-split-header-right pb-0 pl-split-header-left` | Skills list header |
| `split.toolbar-padding` | 20 20 12 28 | `pt-split-toolbar pr-split-toolbar pb-split-toolbar-bottom pl-split-toolbar-left` | Search + filter row block |
| `split.toolbar-gap` | 12 | `gap-split-toolbar` | Search → filter row |
| `split.filter-gap` | 6 | `gap-split-filter` | Filter button → chip → sort |
| `split.list-padding` | 0 12 24 14 | `pt-0 pr-split-list-right pb-split-list-bottom pl-split-list-left` | Scrolling list |
| `detail.padding` | 34 40 48 | `pt-detail-top px-detail pb-detail-bottom` | Detail pane content |
| `detail.max-width` | 900 | `max-w-detail` |  |
| `detail.section-gap` | 28 | `gap-detail-section` | Header → banner → agents → files → details/history |
| `detail.header-gap` | 12 | `gap-detail-header` | Name row (with the action row at its right end) → description |
| `detail.columns-gap` | 40 | `gap-detail-columns` | Details ↔ History |
| `detail.kv-gap` | 12 12 | `gap-detail-kv` | Details key/value grid (row, column) |
| `detail.kv-key-width` | 96 | `w-detail-key` |  |
| `settings.columns` | 240 · up to 560 | `grid-cols-settings` | Settings section: the label column, then the content |
| `settings.column-gap` | 24 | `gap-x-settings-columns` | Settings label column → content |
| `settings.section-padding` | 28 | `py-settings-section` | Above and below each Settings section, between its hairlines |
| `settings.section-gap` | 16 | `gap-settings-section` | Between the rows of a Settings section's content |

### Lists and rows

| Token | px | Class | Element |
| --- | --- | --- | --- |
| `row.padding` | 11 12 11 14 | `py-row pr-row-right pl-row-left` | Split list row |
| `row.gap` | 12 | `gap-row` | Glyph → text |
| `row.line-gap` | 3 | `gap-row-line` | Name row → description |
| `row.inline-gap` | 8 | `gap-row-inline` | Name → pills → chips |
| `row.spacing` | 2 | `gap-rows` | Between rows |
| `row.glyph-col` | 18 | `w-row-glyph` | Glyph column |
| `group.padding-first` | 4 14 8 | `pt-group-first-top px-group pb-group-bottom` | First group header |
| `group.padding` | 20 14 8 | `pt-group-top px-group pb-group-bottom` | Later group headers |
| `group.gap` | 8 | `gap-group` | Label → count |
| `table.header-height` | 40 | `h-table-header` | Sticky header (servers) |
| `table.row-padding` | 16 | `p-table-row` | Server rows |
| `table.col-gap` | 24 | `gap-x-table-col` |  |
| `server.columns` | 1fr 170 130 120 | `grid-cols-server` | The columns of the server header row and rows: SERVER · TRANSPORT · TOOLS · AGENTS |

### Buttons, pills, controls

| Token | px | Class | Element |
| --- | --- | --- | --- |
| `button.primary-height` | 38–40 |  |  |
| `button.primary-padding-x` | 20 | `px-button-primary` |  |
| `button.secondary-height` | 34–38 |  | 34 in the detail pane, 36–38 elsewhere |
| `button.secondary-padding-x` | 14–16 |  |  |
| `button.ghost-height` | 28–36 |  |  |
| `button.ghost-padding-x` | 12–16 |  |  |
| `button.icon-size` | 24–38 |  | Square icon buttons |
| `button.gap` | 8 | `gap-button` | Between sibling buttons (6 in the detail action row) |
| `button.icon-gap` | 7–8 |  | Icon → label inside a button |
| `pill.height` | 18–22 |  | 20 in list rows, 22 in the detail header |
| `pill.padding-x` | 7–10 |  |  |
| `switch.size` | 36 22 | `w-switch h-switch` | Track w h |
| `switch.padding` | 3 | `p-switch` |  |
| `switch.knob` | 16 | `size-switch-knob` |  |
| `switch.compact-size` | 30 18 | `w-switch-compact h-switch-compact` | Inside agent client toggle chips |
| `switch.compact-knob` | 14 | `size-switch-compact-knob` |  |
| `checkbox.size` | 16 | `size-checkbox` |  |
| `segmented.padding` | 2–3 |  | Track padding |
| `segmented.gap` | 2 | `gap-segmented` |  |
| `segmented.height` | 24–28 |  | Segment |
| `segmented.padding-x` | 10–12 |  |  |
| `filter.height` | 30 | `h-filter` | Filter button and agent client chip |
| `filter.badge` | 16 | `size-filter-badge` | Count badge |
| `agent-chip.height` | 44 | `h-agent-chip` | Agent client toggle chip |
| `agent-chip.padding` | 0 8 0 7 | `py-0 pr-agent-chip-right pl-agent-chip-left` |  |
| `agent-chip.gap` | 10 | `gap-agent-chip` | Logo → name → switch |
| `agent-chip.wrap-gap` | 8 | `gap-agent-chip-wrap` | Between chips |
| `logo.overlap` | −6 to −8 |  | Negative margin between stacked logos |
| `logo.ring` | 2 | `ring-logo` | Ring width |
| `logo.padding` | 3–5 |  | Inner padding by size (3 at 20px, 4 at 24–26, 5 at 28) |
| `logo.padding-sm` | 3 | `p-logo-sm` | 16 and 20px logos |
| `logo.padding-lg` | 5 | `p-logo-lg` | 28px logos; a 24px logo takes the `1` spacing step (4px) |
| `logo.padding-xl` | 8 | `p-logo-xl` | 44px logos |

### Inputs

| Token | px | Class | Element |
| --- | --- | --- | --- |
| `input.height` | 42 | `h-input` | Dialog and settings inputs |
| `input.padding-x` | 14 | `px-input` |  |
| `input.search-height` | 36 | `h-input-search` | List search field |
| `input.search-padding` | 0 14 0 34 | `py-0 pr-input-search-right pl-input-search-left` | Room for the icon |
| `input.label-gap` | 8 | `gap-input-label` | Label → field |
| `input.textarea-height` | 88 | `h-textarea` | Create dialog description |
| `add-search.height` | 40 | `h-add-search` | Search field of the Add skills screen, a pill across the page |

### Cards, banners, tiles

| Token | px | Class | Element |
| --- | --- | --- | --- |
| `card.padding` | 16–18 |  | Inbox card |
| `card.gap` | 12 | `gap-card` | Between Inbox cards |
| `card.header-gap` | 14 | `gap-card-header` | Icon tile → text → actions |
| `card.tile` | 34 | `size-card-tile` | Icon tile |
| `card.row-padding` | 10 0 | `py-card-row px-0` | Sub-rows inside the Updates card |
| `banner.padding` | 14 16 14 14 | `py-banner pr-banner-right pl-banner` |  |
| `banner.gap` | 14 | `gap-banner` | Tile → text → actions |
| `banner.tile` | 32–34 |  |  |
| `banner.text-gap` | 3 | `gap-banner-text` | Title → text |
| `agent-card.padding` | 22 | `p-agent-card` | Agents page cards |
| `agent-card.gap` | 12 | `gap-agent-card` | Grid gap |
| `agent-card.logo` | 44 | `size-agent-card-logo` |  |
| `universal-card.lead-width` | 260 | `w-universal-lead` | Left column of the Universal card: logos, "Universal", the library path |
| `plugin-card.padding` | 22 | `p-plugin-card` |  |
| `plugin-card.row-height` | 36 | `h-plugin-card-row` | Tree rows |
| `plugin-card.gap` | 12 | `gap-plugin-card` | Between the masonry columns; each card's wrapper adds 12 below |
| `plugin-card.tile` | 40 | `size-plugin-card-tile` | Plugin icon or monogram tile |
| `add-card.gap` | 12 | `gap-add-card` | Grid gap |
| `add-card.min-height` | 120 | `min-h-add-card` |  |

### Overlays

| Token | px | Class | Element |
| --- | --- | --- | --- |
| `dialog.width` | 480 | `w-dialog` |  |
| `dialog.padding` | 32 | `p-dialog` |  |
| `dialog.gap` | 22 | `gap-dialog` | Between dialog blocks |
| `popover.width` | 300 | `w-popover` | Filter popover |
| `popover.padding` | 8–14 |  | 8 for row lists, 14 for grouped content |
| `popover.gap` | 16 | `gap-popover` | Between groups |
| `popover.row-height` | 32–34 |  |  |
| `popover.offset` | 8 | none: a number passed to the popover's `sideOffset` | Gap from its anchor |
| `menu.min-width` | 240 | `min-w-menu` | Menu |
| `menu.padding` | 6 | `p-menu` | Around the menu's rows |
| `menu.offset` | 4 | none: a number passed to the menu's `sideOffset` | Gap from its trigger |
| `sheet.width` | 420 | `w-sheet` | History sheet |
| `sheet.width-wide` | 560 | `w-sheet-wide` | What's new sheet, whose patches need the room |
| `sheet.inset` | 10 | `top-sheet right-sheet bottom-sheet` | From top/right/bottom |
| `sheet.padding` | 28 32 | `py-sheet-y px-sheet` |  |
| `palette.width` | 640 | `w-palette` |  |
| `palette.input-height` | 60 | `h-palette-input` |  |
| `palette.top` | 112 | `top-palette` | From the window top |
| `palette.list-max-height` | 420 | `max-h-palette-list` | Palette result list |
| `key-hint.padding` | 3 8 | `py-key-hint-y px-key-hint` | Key hint (`esc`, `⌘K`) |
| `toast.height` | 44 | `h-toast` |  |
| `toast.padding` | 0 18 | `px-toast` (`pl-toast` with an action) | 0 6 0 18 with an action |
| `toast.offset` | 24 | none: a number passed to Sonner's `offset` in `Toaster` | From the bottom edge |
| `toast.detail-width` | 440 | `w-toast-detail` | Error toast |
| `toast.detail-padding` | 12 6 12 18 | `py-toast-detail pr-1.5 pl-toast` |  |
| `floating-bar.offset` | 24 | `bottom-floating-bar` | From the bottom edge |
| `floating-bar.height` | 56 | `h-floating-bar` |  |

### Code, diff, timeline, startup

| Token | px | Class | Element |
| --- | --- | --- | --- |
| `code.padding-y` | 14 | `py-code` | Pane top/bottom |
| `code.gutter` | 48 12 | `grid-cols-code` (with the text column) | Line-number and marker columns |
| `code.gap` | 12 | `gap-x-code` | Between gutter and text |
| `code.max-height` | 340 | `max-h-code` | The pane scrolls past it |
| `file-row.height` | 44 | `h-file-row` | File tree row |
| `file-row.padding-x` | 16 | `px-file-row`, `right-file-row` for the control at the row's end |  |
| `hunk.gap` | 12 | `gap-hunk` | Between hunk blocks |
| `hunk.header-padding` | 8 8 8 16 | `py-hunk-header pr-hunk-header pl-hunk-header-left` |  |
| `hunk.pane-margin` | 4 8 | `my-hunk-pane-y mx-hunk-pane` | Around the two panes |
| `diff.segment` | 56 4 | `w-diff-segment h-diff-segment` | Progress segment w h |
| `diff.segment-gap` | 4 | `gap-diff-segment` |  |
| `timeline.col` | 12 | `grid-cols-timeline` (with the text column) | Dot column |
| `timeline.gap` | 12–16 |  | Dot → text |
| `timeline.dot` | 7 | `size-timeline-dot` |  |
| `timeline.line` | 20 | `h-timeline-line` | The box the dot is centred in beside a plain entry: the line height of `type.timeline-entry` |
| `timeline.button-line` | 32 | `h-timeline-button-line` | The box the dot is centred in beside an entry whose first line is a wrapping small button (the expandable entries of the History sheet, the "Show all {n}" row): the button's top padding (6) plus one `timeline.line` |
| `timeline.entry-gap` | 16–20 |  | Bottom padding per entry |
| `check.column-width` | 480 | `w-check-column` | Startup column |
| `check.block-gap` | 36 | `gap-check-block` | Logo/title → checks → actions |
| `check.row-gap` | 22 | `gap-check-row` | Between checks |
| `check.mark` | 20 | `size-check-mark` | Status circle |
| `check.mark-gap` | 14 | `gap-check-mark` | Mark → text |

## 5. Radii

Radii are scoped the same way. Same-named steps across elements are independent.

| Token | px | Element |
| --- | --- | --- |
| `button.radius` | 9999 | All buttons |
| `button.icon-radius` | 6 or 50% | Small icon buttons |
| `pill.radius` | 9999 | Status pills, filter chip, source pills, version pill |
| `switch.radius` | 9999 | Track and knob |
| `checkbox.radius` | 50% | Popover checkbox |
| `segmented.radius` | 9999 | Track and segments |
| `agent-chip.radius` | 9999 | Agent client toggle chip |
| `logo.radius` | 50% | Agent client logo |
| `shell.nav-radius` | 10 | Nav item |
| `shell.logo-radius` | 8 | Sidebar product mark |
| `shell.status-radius` | 9999 | Account remote status |
| `shell.command-radius` | 9999 | Command field |
| `shell.panel-radius` | 14 | Main panel |
| `input.radius` | 12 | Inputs, textareas |
| `input.search-radius` | 9999 | Search field |
| `row.radius` | 12 | Split list rows |
| `table.row-radius` | 14 | Server rows |
| `popover.row-radius` | 10 | Popover rows, Reset button |
| `menu.radius` | 12 | Menu |
| `menu.row-radius` | 8 | Menu rows |
| `card.radius` | 18 | Inbox cards, review file cards |
| `card.tile-radius` | 10 | Icon tile |
| `file-card.radius` | 16 | File tree card |
| `banner.radius` | 18 |  |
| `agent-card.radius` | 20 | Agents page cards |
| `plugin-card.radius` | 20 |  |
| `add-card.radius` | 18 |  |
| `hunk.radius` | 14 | Hunk block |
| `hunk.pane-radius` | 10 | Yours / theirs panes (outer corners only) |
| `diff.segment-radius` | 4 | Progress segment |
| `popover.radius` | 16 |  |
| `dialog.radius` | 24 |  |
| `sheet.radius` | 20 |  |
| `palette.radius` | 20 |  |
| `palette.row-radius` | 12 | Palette rows |
| `key-hint.radius` | 9999 | Key hint |
| `toast.radius` | 9999 |  |
| `toast.detail-radius` | 18 | Error toast |
| `floating-bar.radius` | 9999 |  |
| `check.mark-radius` | 50% | Startup status circle |
| `check.hint-radius` | 8 | Startup check hint callout (`tone.danger` for a failure, `tone.attention` for a warning: bg + 1px border) |
| `startup.logo-radius` | 12 | Product mark on the startup layer |

## 6. Components

### Buttons (height / padding / colours)

- **Primary** – `button.primary-height` / 0 `button.primary-padding-x` / `button.primary-bg`, `button.primary-text`, 600; hover `button.primary-hover`. Disabled: `opacity:.3` and inert (never a grey fill).
- **Secondary** – `button.secondary-height` / 0 `button.secondary-padding-x` / `button.secondary-bg`, `button.secondary-text`, 500; hover `button.secondary-hover`.
- **Ghost** – `button.ghost-height` / 0 `button.ghost-padding-x` / transparent, `button.ghost-text` (or `text.muted`); hover `button.ghost-hover`, `text.primary`.
- **Small, wrapping** – a small button whose label is a sentence (a commit's subject in the History sheet, "Show all {n}"): at least 34 high, 6 `button.ghost-padding-x`, left-aligned, the label wraps in `timeline.line` lines and the button grows with them; never clipped, never scrolled.
- **Danger** – ghost in `button.danger-text`; hover `button.danger-hover`.
- **Icon** – `button.icon-size` square, `button.radius` or `button.icon-radius`, transparent; hover `button.ghost-hover`.
- **Toast action** – ghost with `button.toast-action-text` on `overlay.bg`.

Two buttons side by side: `button.gap`, primary on the right.

### Pills (status)

Height `pill.height`, padding 0 `pill.padding-x`, `pill.radius`, 11–12 / 600.

- Not published (one of your own skills not yet published, on its list row only): `pill.neutral-bg` / `pill.neutral-text`. It is the only status pill a skill row carries.
- Disabled (plugin): `pill.disabled-bg` / `pill.disabled-text`.
- Version (plugin): `pill.version-bg` / `pill.version-text`, mono 12.
- Fork (on the original of a fork made under a new name, in the detail header): `pill.neutral-bg` / `pill.neutral-text`, the 12px fork glyph in the text colour, then the fork's name; hover `pill.neutral-hover`. `title` "Forked as {fork name}"; a click opens the fork. One pill per fork.

A skill row has no other status pill. When the skill's banner (§6 "Banner (detail pane)") has tone `attention` or `danger`, the row shows an alert triangle right after the name and its pill: `AlertTriangleIcon` (an exclamation mark in a triangle; the banner tile keeps the circled `AlertIcon`) at 14px in `tone.attention.fg`, whatever the banner's tone, with `title` and `aria-label` the banner's title, which the row's accessible description also carries. A `primary`, `neutral` or `yours` banner puts no triangle on the row.

### Switch

`switch.size`, `switch.padding`, `switch.radius`; knob `switch.knob` round. On: track `switch.on-track`, knob `switch.on-knob`, `justify-content:flex-end`. Off: track `switch.off-track`, knob `switch.off-knob`. `transition: background .18s`. The whole row is the hit target. Compact variant `switch.compact-size` (knob `switch.compact-knob`) inside agent toggle chips. A switch always shows the state the last inventory reported: while its command runs, and until the inventory that follows it arrives, it is pending (`aria-busy`, disabled at 50% opacity, a `Spinner` beside it) and keeps that state; it never shows the outcome ahead of the CLI. Its `title` says what the state means ("Enabled · new skills go here by default" / "Disabled · skipped by default").

### Agent client toggle chip (detail pane AGENTS)

Pill button `agent-chip.height`, `agent-chip.padding`, `agent-chip.gap`, `agent-chip.radius`: 28px logo(s) · name 13 / 500 · compact switch. Off: `agent-chip.off-bg` with 1px `agent-chip.off-border`. On: `agent-chip.on-bg` with `agent-chip.on-border`. Hover border `agent-chip.hover-border`. Locked: 45% opacity, `cursor:not-allowed`, `title` holds the reason; a click toasts it. Chips wrap in a row with `agent-chip.wrap-gap`. A click on a direct agent's chip runs one command, and the chip is pending (`aria-busy`, `cursor:wait`, a `Spinner` in place of the compact switch) until the command ends and the next inventory arrives; its `title` reads "Add to {Agent}" / "Remove from {Agent}". A displaced chip shows "displaced" in `tone.attention.fg` under the name and runs nothing: a click toasts "Reconnect it first, then manage it from your library". The Universal chip, on, opens the Remove dialog: a universal client reads the library, so a skill leaves it only by leaving the machine.

### Checkbox (popover)

16×16 round, inset ring 1.5px `checkbox.ring`; checked: `checkbox.checked-bg` fill, `checkbox.check` check. The same mark inside a dialog (the Remove dialog's "Also delete it from your account remote"), beside its label in `type.body` `text.primary`, with a line of `type.secondary` `text.muted` under it when the choice needs a word of explanation.

### Segmented control

Track `segmented.track-bg`, `segmented.track-border`, `segmented.radius`, `segmented.padding`, `segmented.gap`. Segments `segmented.height` high, `segmented.radius`, 12–12.5 / 500. Selected segment fills with its meaning colour (`segmented.yours-selected`, `segmented.theirs-selected`, `segmented.both-selected`, `segmented.neutral-selected`) and `on-accent`; others transparent `segmented.text`.

### Agent client logo chip

Round, `logo.bg`, `padding:3–4px`, `background-size:contain`, logo from an image bundled with the app (never fetched from the network). Sizes: 16 (filter chip), 20 (list rows, popover), 24 (server rows), 28 (agent client toggle chips), 44 (agent client cards). Overlap with `logo.overlap` and a `logo.ring`-wide ring (`box-shadow: 0 0 0 2px <parent bg>`). Every logo carries `title="<agent client name>"`. A logo is never dimmed or greyed: a server row shows only the agents that have the server on, and reads "Turned off" (`type.secondary`, `text.muted`) when every agent turned it off. The bundled logos (`src/assets/logos`, listed in its README) are the clients' official marks as the vendors publish them, never marks drawn for the app; a client without a bundled mark gets the monogram below.

### Skill glyph (16px, leading the row)

- dot (filled) – a skill of a shared source; `glyph.enabled` when enabled, `glyph.off` when not.
- pencil – edited locally; `glyph.edited`.
- fork – one of your own skills; `glyph.enabled` when enabled and published, `glyph.off` otherwise. `title` explains the state ("Your skill · Enabled", "Edited locally · Not in any agent client").
- fork marker (12px, after the pill and the alert triangle of a row) – the original of a fork made under a new name, in `text.caption`, one per fork; `title` "Forked as {fork name}". A marker, not a control: the fork is listed once, in YOUR SKILLS.

### Group header (lists)

Mono caption 10.5–11 / `.16em` uppercase `group.label` plus a count in `group.count`. `group.padding-first` for the first group, `group.padding` for the rest.

### Banner (detail pane)

`banner.radius`, `banner.padding`, `tone.*.bg` + 1px `tone.*.border`; `banner.tile` icon tile in `tone.*.fg`; title 14 / 500 `banner.title`; text 13 `banner.text`; actions on the right.

The header shows no status pills; the banner is the status. A banner can cover the origin text beside the name (`coversOrigin` in `src/screens/skills/banners.ts`): the header leaves it out while that banner shows. Today only "Added by {tool}" covers it, since it says where the skill came from; the origin of a skill of a source ("acme/skills") is not what its banner says, so it stays. A new banner that says where the skill came from sets `coversOrigin`.

### Agent client card (Agents page)

One card per agent configuration whose client does not read the library, in a 4-column grid with `agent-card.gap`: `card.bg`, 1px `card.border`, `agent-card.radius`, `agent-card.padding`. Top row: the `agent-card.logo` logo at the left and the Enabled switch (§6 "Switch") at the right. Then the client's name in `type.agent-card-name` `card.title`, its configuration path in `type.technical` `text.caption` on one line with an ellipsis, and "{n} skills · {n} servers · {n} plugins" in `type.secondary` `card.text` with the numbers in `text.primary`, 500. Enabled decides only where later installs and creations go when no agent is named; it never changes a placement, so the counts do not move when the switch does.

### Universal card (Agents page)

One full-width card below the grid, the same surface, for every configuration whose client reads the library. Its left column is `universal-card.lead-width` wide: the clients' logos stacked (`agent-card.logo`, ringed in `card.chip-ring`), "Universal" in `type.agent-card-name`, "Read your shared library" in `type.secondary` `card.text`, and the library path in `type.technical` `text.caption`. Then one column per client behind a 1px `shell.divider` hairline on its left: name, path and "{n} skills · {n} servers", the skill count being the library's. No switch: a universal client sees every library skill whatever its setting.

### Banner actions and dialogs (detail pane)

A banner's action is a small ghost button on its right, one command each: "Add to agents" on the gray "Not in any agent" banner (`tone.neutral`), "Reconnect" on the amber "{Agent} is not managed by agentx" banner, "It owns a copy of the skill, so your library changes don't reach it." The banners of an update come first, after the one of a pending merge, `tone.primary` with the `RefreshIcon` in the tile and two actions, a small ghost See changes, which opens the What's new sheet (§6 "Sheet (What's new)"), then a small primary Update (`skill update <name>`): "New version from {owner/repo}" for an update with a base, {owner/repo} being the update's source, or for one of your own skills with an upstream, the upstream; and "Published from another machine" for what another of your machines published (an update with no base). The first one's text is the paths of the files the update changes, joined by " · ", then " · Your local edits are kept." when the skill holds edits; the second one's is the paths alone. Toasts: "Updated {skill}", "Updated {skill}. Merged with your local edits" when it held edits, "Updated {skill}. Publish when ready." for one of your own. A skill with a pending merge shows neither. After the amber one, in this order: "Added by {tool}" (`tone.neutral`, the tool's product name, "Skills CLI" or "Skillshare", as the origin text and the Installed by detail also name it), "Adopt to manage it from agentx", with Adopt (`adopt --skill <name>`, toast "Adopted {skill}"); "{owner/repo} was removed" (`tone.danger`), "Add {owner/repo} source to check for updates.", with Add source (`source add <url>`, toast "Added {owner/repo} source"); "No longer in {owner/repo}" (`tone.danger`), "It seems this skill was removed from the source.", with Fork, which opens the Fork dialog (§6 "Fork dialog (detail pane)"). Adopt and Update are banner buttons only: a skill row carries no call-to-action pill. A skill the CLI refuses to adopt has no banner: its DETAILS end with an "Adopt" row, "Can't adopt: {reason}" in the CLI's words. A command that is refused because the folder holds another version opens a confirm dialog, "Replace {Agent}'s copy?", with the path in `type.technical` `text.caption` and Cancel · Replace (danger). The action row (`aria-label` "Actions", `button.gap` 6 between its buttons) sits on the title row, pushed to its right end (`margin-left:auto`, centred on the row, wrapping when the row is full), after the name, origin and fork pills. Remove, in the action row, is a danger ghost button that opens the Remove dialog: "Remove {skill}?", the agents it will be deleted from as a list in `type.body` `text.secondary`, and Cancel · Remove (danger). Every button that runs a command is pending until the command ends and the next inventory arrives.

A skill with a pending merge (`pending_merge` in the inventory) shows "Update has conflicts" before any other banner: `tone.attention` with the `AlertIcon`, and the text "Your edits and the update changed the same lines in {n} files. Resolve them in the merge folder, then update again.", {n} being the files of the last `conflict` event the app saw for the skill (the store keeps it, by skill, from every command the runner runs), or without " in {n} files" when it has seen none, as after a restart. Three buttons: Show folder (small ghost) shows `<agentx home>/merges/<name>/<name>` in Finder, agentx home being the `machine` event's `home`, and runs no command; Abort (small danger ghost) opens a confirm dialog, "Abort the update of {skill}?", "Your resolved files in the merge folder are discarded. The skill and the update stay as they were.", Cancel · Abort (danger), whose Abort runs `skill update <name> --abort` (toast "Aborted the update of {skill}"); and Update (small primary) runs `skill update <name>` again, which applies the merge once every file is resolved. An update that still conflicts (exit 4) says "Conflicts in {skill}. Resolve them to finish the update"; with `--all`, "Updated {k} skills. {m} have conflicts" ("1 has conflicts").

Your own skills and edited skills of a source come last, after "Not in any agent", in this order. "Edits not published" (`tone.yours`), for one of your own skills edited since it was published: "Publish to share your latest version.", with Publish, which opens the Publish dialog. "Not published" (`tone.yours`), for one never published: "Only on this machine. Publish it to your account remote." with Publish when an account remote is set, else "Set an account remote to publish it." with Open Settings. "Edited locally" (`tone.attention`), for an edited skill of a source: "{n} files differ from {owner/repo}. Updates won't overwrite them." once `skill diff <name>` has counted them ("1 file differs" for one), and "Updates won't overwrite your edits." before it answers, with Fork, which opens the Fork dialog, and Publish to {owner/repo} when this machine can write to the source.

### Card (Inbox)

`card.bg`, `card.radius`, `card.border`, `card.padding`. Header grid: `card.tile` icon tile (`tone.*`) · title/tag/text · actions. Resolve animation: `opacity 0; transform: translateX(24px) scale(.985)` over 280ms, then removed.

### Plugin card (Plugins)

`card.bg`, `plugin-card.radius`, 1px `card.border`, in 3 CSS columns with `plugin-card.gap`; a card never breaks across columns. Top, `plugin-card.padding`: a `plugin-card.tile` tile on the left, the plugin's own icon from the inventory's data URL as an `<img>` with the name as its alt, or the name's first letter on `button.secondary-bg` when the inventory carries none; the tile is never dimmed, a disabled plugin says so with its pill. On the right the version pill (`pill.version-bg` / `pill.version-text`, `type.version-pill`), "no version" when empty, after a Disabled pill (`pill.disabled-bg` / `pill.disabled-text`) when the agent client turned the plugin off. Then the name in `type.plugin-name` and "{Agent} · {marketplace}" in `text.muted` ("local" when the marketplace is empty). Contents under a `card.border` hairline: the groups SKILLS, SERVERS, HOOKS, each a caption and `plugin-card.row-height` tree rows with a `plugin.connector` line (the last row's stops at the middle), the group's dot, the name, mono meta (`type.technical-sm`, `text.caption`: a server's transport, a hook's event) and, on a skill or server row, a chevron; those rows open the skill in Skills or the server in MCP servers and hover `plugin.row-hover`. A hook row opens nothing.

### Fork dialog (detail pane)

Fork is a secondary small button in the detail pane's action row, before Remove, for a skill of a source, an unmanaged skill in the library and a plugin's skill; never for one of your own skills. It opens the dialog "Fork {skill}" (`type.dialog-title`), whose subtitle (`type.secondary`, `dialog.subtitle`) says what the fork does for that kind of skill. Below it, the switch row "Use a new name" (`type.body` `text.primary`, with "The fork then sits beside {skill} instead of replacing it." in `type.secondary` `text.muted` except for a plugin's skill, whose fork always sits beside it), the whole row the hit target; with it on, a mono Name field (`Input` `mono`) that takes the focus, and under it, while what is typed breaks the naming rule, "Use 1 to 64 lowercase letters, digits and single hyphens" in `type.secondary` `text.hint`. Cancel · Fork (primary); Enter forks, and Fork is disabled while the switch is on and the name breaks the rule. While the command runs, Fork shows a `Spinner` and the dialog stays. A fork that succeeds closes the dialog, toasts "Forked {skill}" and selects the fork. A refusal shows under the field as in the Add source dialog (§6 "Refusal under a field"): the CLI's message then its hint, as one sentence-cased paragraph in `type.notice` `tone.danger.fg`, and exit 6 turns the switch on. After a plugin's skill is forked, the dialog "What each agent sees" lists each agent the CLI wrote a note for: a 20px logo, its name in `type.body` `text.primary` and the note in `type.secondary` `text.muted`, then Done (primary). The Fork of the "No longer in {owner/repo}" banner (§6 "Banner actions and dialogs") is a small ghost button that opens this same dialog.

### Publish dialog (detail pane)

Publish on the "Edits not published" and "Not published" banners, and Publish to {owner/repo} on an "Edited locally" banner whose source this machine can write to, are small ghost buttons that read the changed files with `skill diff <name>` and open the dialog "Publish {skill}" (`type.dialog-title`). Its subtitle (`type.secondary`, `dialog.subtitle`) says where the publish goes: "To your account remote, {url}" for one of your own skills, "To {owner/repo}, as a change under your own Git name" for a skill of a source. Its blocks sit `dialog.gap` apart. Below the subtitle, the "What changed" field (label `type.secondary` `text.muted`, `input.label-gap` above an `Input` with the placeholder "Leave empty and agentx describes the files"), passed as `--message` only when it holds more than spaces. Then FILES (`type.caption` `text.caption`): one row per changed file, the path in `type.technical` `text.secondary` and its status ("edited by you", "added by you", "removed by you") in `type.technical-sm` `file-row.edited-label`; while the read runs "Reading the files…", and "No changed files" or "The changed files could not be read", each in `type.secondary` `text.hint`. Cancel · Publish (primary); Enter publishes. While `skill publish` runs, Publish shows a `Spinner`, the field is disabled, and the footer's left side says "Publishing {skill}…", or "Fetching {owner/repo}…" while a `progress` event of phase `fetch` names a source, in `type.secondary` `text.hint`. The dialog closes when the command ends. Toasts: "Published {skill}"; "Nothing to publish in {skill}" when it was up to date; "{skill} changed somewhere else first. Update it, then publish." with Update (`skill update <name>`) when the remote moved; "Set an account remote to publish" with Open Settings when no account remote is set. Any other failure keeps the runner's error toast, the CLI's message then its hint as one sentence-cased paragraph (§6 "Refusal under a field").

### Toast

`toast.offset` from the bottom, centred; `overlay.bg`, `overlay.border`, `toast.radius`, `toast.height`, `toast.padding`; text 13.5 `toast.text`; optional action button ghost in `button.toast-action-text`. Auto-dismiss 2.6s, or 5s when it has an action. One toast at a time. A toast for a command that runs (Check tools: "Checking tools" with a Cancel) stays until the command ends, when the toast of its outcome replaces it. Check updates is one too: "Checking for updates" with a Cancel, then "Checking for updates · {n} of {total} sources" as each source is fetched, the text changing in place; then "{n} skills have updates" (with Update all, `skill update --all`, when there are more than one), "1 skill has an update" or "Everything is up to date"; a source it could not reach is an error toast, "Couldn't reach {owner/repo}. Verify that you can reach the source." ("Couldn't reach every source. …" when the CLI names none), the app's own hint in place of the CLI's.

### Attention toast

A command that exited 0 and logged warnings (a placement skipped, a copy that differed): the same shape as the error toast below, with the success text in `type.toast` `toast.text` and each warning in `type.notice` `tone.attention.fg`, the toast's action (Undo) when it has one, and a ghost Dismiss. It goes after 5s. The activity entry it logs is `attention`, not `success`.

### Error toast

A command that fails for a reason the app does not handle itself (`src/store/runner.ts`). Same place, `overlay.bg` and `overlay.border` as the toast, with `popover.shadow`, `toast.detail-radius`, `toast.detail-width` and `toast.detail-padding`. The CLI's message and its hint as one paragraph in sentence case (§6 "Refusal under a field"), in `type.toast`, `toast.text`; the lines that say more about what happened below it, as they are, in `type.notice`, `text.muted`; each warning the CLI logged in `type.notice`, `tone.attention.fg`. A ghost Dismiss on the right. It stays until dismissed when it has a hint, and goes after 5s without one. It replaces the toast shown, like any toast.

### Stale bar

A bar across the top of the main panel while the screens show an inventory that may be out of date (`src/app/stale-bar.tsx`): `stale-bar.height`, `stale-bar.padding`, a 1px bottom border, text in `type.notice`, `text.secondary`. While serve restarts: `tone.attention` bg and border, a `Spinner` and "Reconnecting to agentx…". When a scan fails or serve stops for good: `tone.danger` bg and border, "Couldn't read this machine: " and the reason, and a ghost small Retry on the right. Nothing shows while the inventory is fresh.

### Machine label

The machine's label from the inventory, under the product name in the sidebar: `type.machine-label`, `text.muted`, one line with an ellipsis.

### Timeline (history)

Grid `timeline.col 1fr`, `timeline.gap`. Dot `timeline.dot` round (`timeline.dot-install` for an upstream import, `timeline.dot-update` for a commit made with Git, `timeline.dot-yours` for one agentx wrote), centred in a box as high as the entry's first line (`timeline.line` beside the subject text, `timeline.button-line` beside a small button), a 1px `timeline.connector` from the bottom of that box to the next entry. Entry: subject `type.timeline-entry` in `text.primary`, meta `type.timeline-meta` in `timeline.meta` ("{date} · {n} files", then "made with Git" or "from the original"). Final item when truncated: hollow dot (inset ring 1.5px `timeline.more-ring`) and a ghost button "Show all {n}", which opens the History sheet.

### Menu

A list of actions under a button (`src/components/ui/dropdown-menu.tsx`): `menu.min-width`, `menu.radius`, `overlay.bg` with 1px `overlay.border` and `menu.shadow`, `menu.padding` around the rows, `menu.offset` below its trigger and aligned to its end. A row is `menu.row-radius` with `menu.row-hover` when pointed at or focused; its lines stack: a `type.body` line in `text.primary`, then a `type.technical-sm` line in `text.caption` such as a path. Escape or a click outside closes it. No animation.

### Split button

Two ghost icon buttons that meet with no gap and share one outline, told apart by a 1px `button.split-divider` on the chevron's left edge: the action (`button.icon-size` square, rounded at its left end only) and a narrower chevron pointing down that opens the action's menu (rounded at its right end only). Only the action shows when there is nothing to choose. Show in Finder on a file row is one.

### Popover (Filter)

`popover.width`, `popover.radius`, `overlay.bg` with 1px `overlay.border` and `popover.shadow`, 14px padding, `popover.gap` between its groups, `popover.offset` below its anchor. Each group opens with a caption in `popover.caption`. SHOW is a row of pills (`type.secondary`, count in `type.technical-sm` `text.caption`; the chosen one `button.secondary-bg` and `text.primary`), STATE a segmented control, AGENTS rows of `popover.row-radius` with the round checkbox, a 20px logo and the name. Reset filters is a ghost button, shown while a filter is applied. A click outside or Escape closes it.

### Sheet (History)

`sheet.width`, inset `sheet.inset` from the top, right and bottom, `sheet.bg` with 1px `sheet.border`, `sheet.radius`, `overlay.shadow`, `sheet.padding`, over `shell.scrim`. Title in `type.dialog-title`, the subject's name under it in `type.technical` `text.caption`, a ghost Close at the top right. Escape or Close closes it. Its content scrolls down only: a long line wraps, it never clips or scrolls sideways.

Each entry is a small wrapping ghost button with a chevron that expands it, and collapses it again: under the meta line, the files its commit changed, one row each with the status (`type.technical-sm`, `text.caption`) and the path (`type.technical-sm`, `text.secondary`).

### Sheet (What's new)

The History sheet's frame at `sheet.width-wide`, for what an update changes before the user takes it in. Title "What's new", the skill's name under it in `type.technical` `text.caption`, then the base and the newer version in `type.technical-sm` `text.caption`, "{base} → {newer}" (each by its date when the CLI dates it, else by its short id; the newer alone for an update from another machine). Below, the File list (§6) with one row per file `skill diff <name> --update` emits, every one open: the status in `type.technical-sm` (`text.caption`, `deleted` in `accent.danger`), the path in `type.technical` `file-row.path`, and the file's patch in the Code pane with the diff tints, its hunks alone, numbered as in the newer version. A patch with no lines to show says "Binary file" or "No line changes" in `type.secondary` `text.hint` on `code.bg`. While the read runs the sheet says "Reading the changes…"; a failed read says "The changes could not be read" with the CLI's message under it in `type.technical-sm` `text.caption`, and no error toast. An update from another machine cannot be diffed: the sheet says "Changes from another machine can't be shown before you update." in `type.secondary` `text.hint`, then lists the files of the update check in the same card, status and path, with no chevron and nothing to open. Escape or Close closes it.

### Agent client logo chip, monogram

A client with no bundled logo shows the first letter of its name in `type.pill`, `on-accent` on `logo.bg`, drawn as an image like every other logo.

### Code pane

`code.bg`, mono 12.5, `line-height:1.75`, grid `code.gutter 1fr` (line no. `code.line-number` right-aligned · marker · text). Front-matter and keys `code.frontmatter`, headings `code.heading`, list items `code.list`, prose `code.prose`. Diff tints: added line `+` on `diff.added-bg`, removed line `−` on `diff.removed-bg` with no number. On a tinted line, front matter takes `code.prose` so it keeps 4.5:1.

### File list (detail pane FILES)

A `card.bg` card, `file-card.radius`, 1px `card.border`, holding one row per file, `file-row.height` high with `file-row.padding-x`, a 1px `file-row.divider` hairline between rows and `file-row.hover` on hover or focus: a chevron (`rotate(90deg)` in .15s when the row is open), the path in `type.technical` (`file-row.path`, `file-row.path-open` while open), a `file-row.edited-label` label for a file the user changed, and "{n} lines", "binary" or "Too large to show ({size})" in `file-row.meta` at the right. SKILL.md comes first and open; the rest follow by path, a file the user removed among them. An open row shows the file in the Code pane below it. A row of a file on disk also has a "Show in Finder" split button, beside the trigger and never inside it, over the meta text at the row's end: while the row is hovered, holds the focus or has its menu open, the meta fades out and the button fades in at that spot, both in .15s, and the row's layout never changes (the meta stays laid out, only its opacity moves); it is in the tab order throughout. A row the edits removed has no button and keeps its meta. The button opens the file at the skill's first place, the library for a skill the library holds, else its first placement in the order the row lists its agents; the chevron's menu lists every other place, one row per placement, "{Agent name}", "{Agent name} ({plugin} plugin)" for a plugin's copy, with the path as the inventory carries it, and opens the file there. A skill with one place has no chevron.

### Key hint

`esc` in the palette and `⌘K` / `Ctrl K` in the command field (`src/components/key-hint.tsx`): `key-hint.padding`, `key-hint.radius`, `button.secondary-bg` fill, `type.key-hint` in `text.muted`.

### Inputs

`input.height`, `input.bg`, `input.border`, `input.radius`, padding 0 `input.padding-x`, 14px; focus `input.border-focus`. Mono for URLs and commands. Search field in lists: `input.search-height`, `input.search-radius`, `input.search-padding`, icon at left, placeholder `input.placeholder`.

### Create skill dialog

The Skills header's Create and the Add skills header's Create skill open it (`src/screens/skills/create-dialog.tsx`): title "Create skill", subtitle "Creates a skill of your own in your library and adds it to your agents." in `dialog.subtitle`. Two fields, each with its label in `type.secondary` `text.secondary` and a hint under it in `type.secondary` `text.hint`. Name, mono, focused on open, hint "1 to 64 lowercase letters, digits and single hyphens". Description, hint "What it does and when to use it, in one or two sentences. Up to 1024 characters.", counted in characters (the Agent Skills specification gives a description 1 to 1024 characters; `skill new --description` takes one line of at most 1024 bytes). A hint whose rule the field breaks turns `tone.danger.fg`, the field is `aria-invalid` and Create is disabled; a name the CLI refuses (exit 6) shows its message and hint under Name in place of the rule. Cancel (ghost) · Create (primary), pending until the inventory that follows lists the new skill; toast "Created {name} skill". Any other refusal shows under Description. Both are a refusal under a field (below).

### Refusal under a field

When the CLI refuses what a dialog's field holds (Create skill, Add source), its message and its hint show under the field as one paragraph, in sentence case, the message then the hint, in `type.notice` `tone.danger.fg` (`src/components/problem-text.tsx`, an alert): "The account repo already holds release-notes as one of your own skills. Choose another name." The app capitalises the first letter of each and ends each with a full stop, never doubling a full stop, question mark or exclamation mark already there, and changes nothing else: a quoted command stays as the CLI wrote it, and a sentence that starts with a name ("agentx", a URL or path, a skill's name) keeps its case. The error toast says its message and hint the same way.

### Settings

The Settings page (`src/screens/settings`): the serif page title, then one section per area, a hairline (`shell.divider`) between two sections and none above the first. A section is a `settings.columns` grid with `settings.column-gap`, `settings.section-padding` above and below: the label column holds the section label in `type.settings-label` `text.primary` and, under it, one line in `type.secondary` `text.muted`; the content column holds the section's rows, `settings.section-gap` apart. Inputs are `input.height`; paths and ids are `type.technical`, ids in `text.hint`. A switch row is the setting's name in `type.body` `text.primary` and its line in `type.secondary` `text.muted`, with the switch (§6 "Switch") at the right; the whole row is the hit target. A failure under a field is the CLI's message in `type.notice` `tone.danger.fg`, its hint under it in `text.muted`; under the Account remote field, the message then the hint as one sentence-cased paragraph (§6 "Refusal under a field").

## 7. Iconography

Stroke icons on a 24 grid, `stroke-width` 1.75–2.25, round caps/joins, 14–18px rendered at the sizes below. Fill only for the "dot" glyph. No icon fonts, no emoji.

An icon is a hand-written React component that takes the text colour; an illustration (such as the agentx mark) is an SVG file in `src/assets` shown as `<img>`.

Sizes: `xs` 14px, `sm` 15px, `md` 16px (the default), `lg` 18px.

The product mark is `shell.logo-mark` on `shell.logo-bg`, 30px `shell.logo-radius` in the sidebar (the height of the product name and machine label beside it), 40px `startup.logo-radius` on the startup layer, with a 1px `shell.logo-ring` ring. Its SVG is also the source of the app icons: after changing it, run `pnpm tauri icon src/assets/agentx-mark.svg` and keep only the files `src-tauri/tauri.conf.json` lists. The SVG holds the two colours as hex values (`lime-400`, `gray-990`); change them together with the tokens.

## 8. Motion

- Hover/background changes: no transition or ≤ .18s.
- Switches: `.18s` background.
- Chevrons: `rotate(90deg)` with `.15s`.
- File row control: the meta text and the Show in Finder button swap by opacity in `.15s` on hover.
- Floating bar (`animate-floating-bar`): `translateY(24px) → 0`, opacity 0 → 1, `.22s ease`, when it appears.
- Card resolve: 280ms, see §6.
- Spinner-style icons (Check updates, Check tools): one full rotation `.9–1s ease` (`animate-turn-once`: 1s ease, once, while the command runs), then a toast.
- Startup check running (`animate-check-spin`): the status ring turns once a second, linear, while its check runs, and stops when the check settles.
- Screens fade in (`animate-fade-in`): opacity 0 → 1 over `.2s ease-out` when they mount, once the startup checks pass. Nothing waits on purpose: a passing launch shows no startup layer. A launch whose checks run for over 2s shows a `Spinner` and what the checks do (`type.secondary`, `text.muted`) in the middle of the empty panel, faded in the same way, until the checks settle: "Checking this machine" while `version` and `doctor` run, then "Looking for skills", "Looking for MCP servers" and "Looking for plugins" in turn, each for 1.5s and faded in, while serve scans. From 5s into a check, a line below (`type.row-desc`, `text.hint`) counts down to its deadline: "Taking longer than usual · trying for 25s more".
- Diff pane choice: `opacity .2s, box-shadow .2s`.
- Primary button enable/disable: `opacity .2s`. Nothing loops forever.

## 9. Copy rules

- Plain verbs: Add, Remove, Update, Fork, Publish, Review update, Reconnect, Adopt, Check updates.
- Copy uses the terms in [`GLOSSARY.md`](../../GLOSSARY.md) and never a word the glossary lists under _Avoid_. Where a glossary term names the thing, use that term, such as "account remote", "account repo", "upstream" or "base version". Where this file and the glossary differ, the glossary wins.
- Named exception: the "Agents" screen name, its nav label and the "AGENTS" captions stay as they are, although the glossary avoids "agent" for an agent client.
- For a git word the glossary has no term for, use a plain word: "change", not "hunk".
- "Your skills" for the user's own skills (forked or created); "its upstream" for where a forked skill came from; "the update" for an update candidate, a newer version from a skill's source. Fork is an action, not a kind of skill.
- Dates over ids in rows ("Sep 14 → Sep 28"); ids with dates in Details ("3f2a9c1 · Sep 14").
- Toasts state what happened in past tense ("Commit updated", "Reconnected pdf to Cursor"), and name the undo when one exists.
- Captions are uppercase mono; everything else sentence case. No trailing periods in titles, pills or buttons.
