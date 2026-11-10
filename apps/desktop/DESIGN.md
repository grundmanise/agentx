# agentx desktop: design system

The design decisions of the desktop app, for the people and agents who build it: character, palette and tokens, type, layout, radii, the structure and states of components, iconography and motion. Each entry records a decision and, where it is not obvious, its reason. This file never holds UI copy: the words the app shows live in the code and the specs, and the copy rules in the README's "Rules for components".

Code uses the semantic tokens below, never a primitive or a raw value. The tokens live in `src/styles/tokens.css`, edited by hand, which is the source of truth. When you add or change a token, edit `tokens.css` and its table here in the same change. The tables use dotted names, and `tokens.css` names them like this:

- A colour token `shell.bg` is `--color-shell-bg`, a Tailwind colour (`bg-shell-bg`, `text-text-muted`). Primitives are `--gray-990` and so on, outside the Tailwind theme, so no utility class can name one.
- A tone `tone.primary` is `--color-tone-primary-fg`, `-bg` and `-border`.
- A shadow token `overlay.shadow` is `--shadow-overlay` (`shadow-overlay`).
- A radius token `card.tile-radius` is `--radius-card-tile` (`rounded-card-tile`).
- A layout token `shell.sidebar-width` is `--width-sidebar` (`w-sidebar`). §4 explains the naming and lists the class for each.
- A type token `type.page-title` is the utility `type-page-title`, which sets the face, size, weight, line height, tracking and case together.

The Tailwind theme holds only these tokens. `tokens.css` resets Tailwind's whole default theme (`--*: initial`), so no default colour, spacing step, radius, shadow, font, font size, weight, line height, tracking, breakpoint or animation exists. Besides the tokens, the theme has three font weights (`font-normal`, `font-medium`, `font-semibold`) and the spacing steps of §4. A token given as a range (`38–40`) is guidance with no variable: pick the value the component calls for, or add a token with one value when a component needs it.

## 1. Character

A dark, quiet desktop tool. One accent (lime) carries the accent and action signals; everything else is near-black and grey. Serif for page titles only; a mono face for anything technical (paths, ids, dates, captions). Surfaces are flat: elevation comes from one shade step and, on floating layers, a soft black shadow. No gradients, no emoji, no icon-per-row decoration.

## 2. Color

Two layers. **Primitives** are the raw palette and never appear in component code. **Semantic tokens** name a role and point at one primitive (`--color-shell-bg: var(--gray-990)`); the rest of this file and the app's code use semantic names only.

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

Grouped by element. Each token points at one primitive, or at a meaning token when the value is shared by design. Two elements with a look-alike value still get their own token, so one can change without the other.

#### Meaning (accents and tones)

| Token | Primitive | Meaning |
| --- | --- | --- |
| `accent.primary` | `lime-400` | Primary action, enabled, update candidate found, success, selection |
| `accent.primary-hover` | `lime-300` | Primary button hover |
| `accent.attention` | `amber-400` | Edited, displaced, needs a look but not broken |
| `accent.danger` | `red-400` | Removed, unreachable, destructive |
| `accent.yours` | `violet-400` | Your own skills, your side of a diff, publish |
| `accent.theirs` | `blue-400` | The base version or update side of a diff |
| `on-accent` | `gray-990` | Text and icons on any accent or inverse fill |

Tone triples `{ fg, bg, border }` for banners, notices and icon tiles:

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
| `text.hint` | `gray-400` | Helper text, Details keys, overflow counts |
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
| `shell.status-pending` | `accent.yours` | Account remote status dot, skills to publish (halo `violet-400/18`) |
| `shell.status-published-halo` | `0 0 0 3px lime-400/15` | The halo around the published dot (`shadow-status-published`) |
| `shell.status-pending-halo` | `0 0 0 3px violet-400/18` | The halo around the to-publish dot (`shadow-status-pending`) |
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
| `pill.update-bg` | `accent.primary` | Update pill |
| `pill.update-text` | `on-accent` |  |
| `pill.neutral-bg` | `gray-860` | Not-published pill, fork pill, a source's pin |
| `pill.neutral-hover` | `gray-800` |  |
| `pill.neutral-text` | `gray-200` |  |
| `pill.removed-bg` | `red-400/12` | Removed-from-source pill |
| `pill.removed-text` | `accent.danger` |  |
| `pill.disabled-bg` | `amber-400/10` | Disabled plugin pill |
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
| `segmented.yours-selected` | `accent.yours` | Your side chosen (label `on-accent`) |
| `segmented.theirs-selected` | `accent.theirs` | Base version or update side chosen |
| `segmented.both-selected` | `gray-50` | Both sides kept |
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
| `group.divider-label` | `gray-500` | Caption of the sub-group of skills in no agent client |
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
| `add-card.status-added` | `gray-400` | Card status: already added |
| `add-card.status-forked` | `accent.yours` | Card status: forked |
| `add-card.status-disk` | `accent.attention` | Card status: already in the library |
| `add-card.status-in-use` | `gray-400` | Card status: name in use |
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
| `popover.caption` | `gray-500` | Group captions |
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
| `file-row.meta` | `gray-500` | Line count or size at the row's end |
| `file-row.edited-label` | `accent.attention` | Status of a file the user changed |
| `diff.added-bg` | `lime-400/12` | Added line tint (marker `accent.primary`) |
| `diff.removed-bg` | `red-400/8` | Removed line tint (marker `accent.danger`) |
| `diff.yours-label` | `accent.yours` | Your side's label, dot and chosen-pane ring |
| `diff.yours-bg` | `violet-400/6` | Your side's pane ground |
| `diff.theirs-label` | `accent.theirs` | Base version or update side's label, dot and chosen-pane ring |
| `diff.theirs-bg` | `blue-400/6` | Base version or update side's pane ground |
| `diff.file-pending` | `accent.attention` | File dot while changes remain |
| `diff.file-done` | `accent.primary` | File dot when every change is decided |
| `diff.progress-done` | `accent.primary` | Progress segment, decided |
| `diff.progress-pending` | `gray-850` | Progress segment, pending |

#### Timeline (History)

| Token | Primitive | Element |
| --- | --- | --- |
| `timeline.dot-install` | `gray-600` | Added / found / created entries |
| `timeline.dot-update` | `accent.primary` | Updated, adopted, put back |
| `timeline.dot-yours` | `accent.yours` | Published, forked |
| `timeline.connector` | `gray-890` | 1px line between dots |
| `timeline.meta` | `gray-500` | Relative time under an entry |
| `timeline.more-ring` | `gray-700` | Hollow dot ring of the item that opens the full history |

#### Startup checks

| Token | Primitive | Element |
| --- | --- | --- |
| `check.pass-bg` | `lime-400/12` | ✓ mark tile |
| `check.pass-mark` | `accent.primary` |  |
| `check.fail-bg` | `red-400/12` | Failure mark tile |
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
| `server.tools` | `gray-200` | Tool count |
| `server.tools-auth` | `accent.attention` | A server that needs sign-in |
| `server.tools-fail` | `accent.danger` | A server whose handshake failed |
| `server.tools-none` | `gray-500` | A server not checked yet |
| `server.versions` | `accent.attention` | Version count when agent configurations disagree |

### Contrast rules

Every text colour token reaches at least 4.5:1 (WCAG AA) on the backgrounds it is used on: the shell, page, card, sheet, overlay (palette, dialog, popover, toast), input and code backgrounds, the row and table hover fills, and the button and pill fills. The tightest pairs are `text.caption` (`gray-500`) on `row.hover` at 4.54:1 and `text.muted` (`gray-300`) on `palette.row-active` at 4.69:1.

- Body text is `text.primary`–`text.muted`. `text.hint` and `text.caption` are for helper text, keys, captions, counts, meta and placeholders.
- `text.hint` and `text.caption` reach 4.5:1 on the surfaces and hover fills above, not on lighter fills. On `palette.row-active`, a button fill or a pill, use `text.muted` or lighter: a palette row's hint turns `text.muted` while the row is selected.
- `gray-500` is the darkest grey for text a person needs to read. `text.disabled` and `text.faint` (`gray-600`, `gray-700`) stay below 4.5:1, so use them only for disabled controls, which WCAG exempts, and for line numbers and marks that are not content.
- Check a new pairing before you use it, and keep it at 4.5:1 or more.
- Never put alpha-muted text on accent fills.

## 3. Typography

Fonts (bundled with the app from `@fontsource` packages; the app never loads anything from the network): **Instrument Serif** 400 (titles), **Geist** 400–700 (UI), **Geist Mono** 400–600 (technical).

Each row is a type token: a role with its face, size, weight, line height, tracking and case, and a `type-*` utility in `tokens.css`. Text colour is a separate colour token (§2.2). An empty cell inherits.

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
| `type.status` | sans | 13 | 500 |  |  |  | Publish status label in the title bar (`shell.topbar-text`) |
| `type.palette-input` | sans | 17 | 400 |  |  |  | Palette search input |
| `type.palette-hint` | mono | 12 | 400 |  |  |  | Hint at the end of a palette row, keys in the palette footer |
| `type.palette-footer` | sans | 12 | 400 |  |  |  | Palette footer (`text.hint`) |
| `type.toast` | sans | 13.5 | 400 |  |  |  | Toast message |
| `type.toast-action` | sans | 13 | 600 |  |  |  | Action inside a toast |
| `type.detail-desc` | sans | 14.5 | 400 | 1.5 |  |  | Detail pane description (`text.muted`, `text-wrap:pretty`) |
| `type.banner-title` | sans | 14 | 500 |  |  |  | Banner title (`banner.title`); its text is `type.secondary` in `banner.text` |
| `type.agent-chip` | sans | 13 | 500 |  |  |  | Agent client name in a toggle chip |
| `type.agent-card-name` | sans | 16 | 600 |  |  |  | Agent client name on an Agents page card, and the Universal card's title |
| `type.timeline-entry` | sans | 13.5 | 400 | 20px |  |  | Timeline entry subject (`text.primary`); its line height is `timeline.line`, so the dot beside it sits on the first line |
| `type.timeline-meta` | sans | 12 | 400 |  |  |  | Timeline entry meta (`text.caption`) |
| `type.notice` | sans | 13 | 400 | 1.5 |  |  | Stale bar text; hint and warnings in an error toast |
| `type.server-name` | sans | 14.5 | 500 |  |  |  | Server name in a server row (`text.primary`) |
| `type.transport-pill` | mono | 11.5 | 500 |  |  |  | Transport pill (`server.transport-text` on `server.transport-bg`) |
| `type.settings-label` | sans | 14 | 500 |  |  |  | Settings section label (`text.primary`); the line under it is `type.secondary` in `text.muted` |
| `type.plugin-name` | sans | 17 | 600 |  | -0.01em |  | Plugin name on a plugin card |
| `type.version-pill` | mono | 12 | 500 |  |  |  | A plugin's version pill, a source's pin |

Text wrapping: `text-wrap:pretty` on multi-line text. Single-line cells use `white-space:nowrap; overflow:hidden; text-overflow:ellipsis`.

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
| `universal-card.lead-width` | 260 | `w-universal-lead` | Lead column of the Universal card |
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
| `toast.min-height` | 44 | `min-h-toast` | The height of a one-line toast |
| `toast.max-width` | 640 | `max-w-toast` | Past it the text wraps; leaves 230 on each side of the 1100 minimum window |
| `toast.padding` | 10 18 | `py-toast-y pl-toast` | 10 6 10 18: the action sits 6 from the right edge |
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
| `timeline.button-line` | 32 | `h-timeline-button-line` | The box the dot is centred in beside an entry whose first line is a small wrapping button: the button's top padding (6) plus one `timeline.line` |
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
| `shell.status-dot-radius` | 50% | The publish status dot (`rounded-shell-status-dot`) |
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

Each entry gives a component's structure, tokens and states, and the reason for a choice that is not obvious. What a component says, and which command a control runs, is in its code.

A control that runs a command is pending from the command until the inventory that follows it arrives: `aria-busy`, a `Spinner` in or beside it, and no input. It keeps the state the last inventory reported, so the screen never shows the outcome ahead of the CLI.

Finder in this file means the platform's file manager. `src/lib/reveal.ts` names it for the platform `src/lib/platform.ts` reads; a screen never checks the platform itself.

### Buttons (height / padding / colours)

- **Primary** – `button.primary-height` / 0 `button.primary-padding-x` / `button.primary-bg`, `button.primary-text`, 600; hover `button.primary-hover`. Disabled: `opacity:.3` and inert, never a grey fill.
- **Secondary** – `button.secondary-height` / 0 `button.secondary-padding-x` / `button.secondary-bg`, `button.secondary-text`, 500; hover `button.secondary-hover`.
- **Ghost** – `button.ghost-height` / 0 `button.ghost-padding-x` / transparent, `button.ghost-text` (or `text.muted`); hover `button.ghost-hover`, `text.primary`.
- **Small, wrapping** – for a label that is a sentence, such as a commit subject in the History sheet: at least 34 high, 6 `button.ghost-padding-x`, left-aligned; the label wraps in `timeline.line` lines and the button grows with them, never clipped or scrolled.
- **Danger** – ghost in `button.danger-text`; hover `button.danger-hover`.
- **Icon** – `button.icon-size` square, `button.radius` or `button.icon-radius`, transparent; hover `button.ghost-hover`.
- **Toast action** – ghost with `button.toast-action-text` on `overlay.bg`.

Two buttons side by side: `button.gap`, primary on the right.

### Pills (status)

Height `pill.height`, padding 0 `pill.padding-x`, `pill.radius`, `type.pill`; a plugin's version and a source's pin take `type.version-pill`.

- Not published: `pill.neutral-*`. The only status pill a skill row carries.
- Disabled (plugin): `pill.disabled-*`.
- Version (plugin): `pill.version-*`.
- Fork: `pill.neutral-*`, hover `pill.neutral-hover`, the 12px fork glyph then the fork's name. In the detail header of the original of a fork made under a new name, one per fork; it opens the fork.

A skill row whose banner (§6 "Banner (detail pane)") has tone `attention` or `danger` shows an `AlertTriangleIcon` (`xs`) in `tone.attention.fg` after the name and pill, whatever the banner's tone; its accessible name is the banner's title. The banner tile keeps the circled `AlertIcon`. Other banner tones put nothing on the row.

### Switch

`switch.size`, `switch.padding`, `switch.radius`; round knob `switch.knob`. On: `switch.on-track`, `switch.on-knob`, knob at the end. Off: `switch.off-track`, `switch.off-knob`. Compact variant `switch.compact-size` (knob `switch.compact-knob`) inside agent client toggle chips. In a row with a label, the whole row is the hit target. Pending: disabled at 50% opacity with a `Spinner` beside it.

### Agent client toggle chip (detail pane AGENTS)

Pill button: `agent-chip.height`, `agent-chip.padding`, `agent-chip.gap`, `agent-chip.radius`; a 28px logo, the name in `type.agent-chip`, a compact switch. Chips wrap with `agent-chip.wrap-gap`.

| State | Look |
| --- | --- |
| Off | `agent-chip.off-bg`, 1px `agent-chip.off-border` |
| On | `agent-chip.on-bg`, 1px `agent-chip.on-border` |
| Hover | border `agent-chip.hover-border` |
| Pending | `cursor:wait`, a `Spinner` in place of the switch |
| Locked | 45% opacity, `cursor:not-allowed`; a click toasts the reason |
| Displaced | a `type.technical-sm` line in `tone.attention.fg` under the name; a click runs nothing and toasts why |

The Universal chip, on, opens the Remove dialog: a universal client reads the library, so a skill leaves it only by leaving the machine.

### Checkbox (popover)

16×16 round, inset ring 1.5px `checkbox.ring`; checked: `checkbox.checked-bg` fill, `checkbox.check` mark. In a dialog the same mark sits beside a `type.body` `text.primary` label, with a `type.secondary` `text.muted` line under it when the choice needs explaining.

### Radio group

One choice among a few (`src/components/ui/radio-group.tsx`): a column of rows `gap-2.5` apart, each the round mark of §6 "Checkbox (popover)", checked on the one chosen, then the label in `type.body` `text.primary`, `gap-2.5` between them. The whole row is the hit target, and the arrow keys move the choice.

### Segmented control

Track `segmented.track-bg`, `segmented.track-border`, `segmented.radius`, `segmented.padding`, `segmented.gap`. Segments `segmented.height`, `segmented.radius`, `type.segment`. The selected segment fills with its meaning colour (`segmented.*-selected`) and `on-accent`; the others are transparent in `segmented.text`.

### Agent client logo chip

Round, `logo.bg`, inner padding by size (`logo.padding*`), the image contained. Sizes: 16 (filter chip), 20 (list rows, popover), 24 (server rows), 28 (toggle chips), 44 (agent client cards). Stacked logos overlap by `logo.overlap` with a `logo.ring` ring in the parent's background. Every logo carries the client's name as its `title`.

- The logos are bundled (`src/assets/logos`) and never fetched. Each is the client's official mark as the vendor publishes it, never one drawn for the app.
- A logo is never dimmed or greyed. A client that is off says so in text and in the title, and a server row shows only the agents that have the server on.

### Agent client logo chip, monogram

A client with no bundled logo shows the first letter of its name in `type.pill`, `on-accent` on `logo.bg`, drawn as an image like every other logo.

### Skill glyph (16px, leading the row)

- dot (filled) – a skill of a shared source; `glyph.enabled` when enabled, `glyph.off` when not.
- pencil – edited locally; `glyph.edited`.
- fork – one of your own skills; `glyph.enabled` when enabled and published, `glyph.off` otherwise.
- fork marker (12px, after the pill and the alert triangle) – the original of a fork made under a new name, in `text.caption`, one per fork. A marker, not a control: the fork is listed once, under your own skills.

### Group header (lists)

`type.caption` in `group.label`, then the count in `group.count`. `group.padding-first` for the first group, `group.padding` for the rest.

### Banner (detail pane)

`banner.radius`, `banner.padding`, `tone.*.bg` with a 1px `tone.*.border`; a `banner.tile` icon tile in `tone.*.fg` (the `RefreshIcon` for an update, the `AlertIcon` otherwise); title `type.banner-title` `banner.title`, text `type.secondary` `banner.text`; actions on the right.

- The detail pane shows one banner, the first that applies in the ordered table in `src/screens/skills/banners.ts`. The header shows no status pills: the banner is the status.
- A banner that says where the skill came from sets `coversOrigin`, and the header then leaves out the origin text beside the name. Today only the banner of a skill another tool added does.

| Banner, by what it reports | Tone |
| --- | --- |
| An update that conflicts with local edits | `attention` |
| An update | `primary` |
| A client that holds its own copy instead of the library's | `attention` |
| A skill another tool added | `neutral` |
| A source that is gone, or a skill gone from its source | `danger` |
| A skill in no agent | `neutral` |
| One of your own skills with unpublished edits, or never published | `yours` |
| A skill of a source edited locally | `attention` |

### Banner actions and dialogs (detail pane)

- A banner's actions are small buttons on its right, one command or one dialog each: ghost by default, primary for the action that applies the update, danger ghost for one that discards work. A destructive action opens a confirm dialog first.
- Adopt and Update are banner buttons only: a skill row carries no call-to-action pill. A skill the CLI refuses to adopt has no banner; the reason is a row of its DETAILS.
- A command refused because the target folder holds another version opens a confirm dialog, with the path in `type.technical` `text.caption` and a danger confirm.
- The action row sits on the title row, pushed to its right end (`margin-left:auto`), centred on the row and wrapping when it is full, with `button.gap` 6 between its buttons. Remove, last, is a danger ghost button; its dialog lists the agents the skill leaves in `type.body` `text.secondary`, with a danger confirm.

### Dialog

`dialog.width`, `dialog.padding`, `dialog.radius`, `overlay.bg` with 1px `overlay.border` and `overlay.shadow`, over `shell.scrim-strong`; blocks `dialog.gap` apart. Title `type.dialog-title` `dialog.title`, subtitle `type.secondary` `dialog.subtitle`. The footer ends with a ghost cancel and the confirm button (primary, or danger for what cannot be undone); Enter confirms. While its command runs, the confirm shows a `Spinner` and the dialog stays open. A field label is `type.secondary`, `input.label-gap` above the field; a hint under it is `type.secondary` `text.hint`. A refusal of what a field holds shows under the field (§6 "Refusal under a field"), and a change to the form clears it.

### Fork dialog (detail pane)

Fork is a secondary small button in the action row, before Remove, and a small ghost button on the banner of a skill gone from its source. The dialog holds a switch row (§6 "Switch") to fork under a new name; with it on, a mono Name field appears and takes the focus, its naming rule under it in `text.hint` while the name breaks it, and the confirm is disabled. A fork that succeeds closes the dialog and selects the fork. After a plugin's skill is forked, a second dialog lists each agent the CLI wrote a note for: a 20px logo, the name in `type.body` `text.primary`, the note in `type.secondary` `text.muted`, and one primary button.

### Edit (detail pane)

Edit is a secondary small button, first in the action row, for a skill the user may write to (`canEdit` in `src/screens/skills/edit.ts`). An unmodified skill of a source has none, because its first edit would quietly turn it into one an update has to merge with. Edit opens the skill's `SKILL.md` in the editor chosen in Settings, runs no command and shows no toast; the inventory that follows shows what changed. When the editor cannot be opened, a toast says so, with Show in Finder as its action.

### Publish dialog (detail pane)

Under the subtitle, an optional message field, then FILES (`type.caption` `text.caption`): one row per changed file, the path in `type.technical` `text.secondary` and its status in `type.technical-sm` `file-row.edited-label`. While the files are read, or when there are none or they cannot be read, one line in `type.secondary` `text.hint` takes their place. While the publish runs the field is disabled and the footer's left side shows its progress in `type.secondary` `text.hint`; the dialog closes when the command ends and its outcome is a toast.

### Create skill dialog

`src/screens/skills/create-dialog.tsx`. Two fields, each with its label in `type.secondary` `text.secondary` and its rule as a hint under it: Name, mono and focused on open, and Description. A hint whose rule the field breaks turns `tone.danger.fg`, the field is `aria-invalid` and the confirm is disabled. A name the CLI refuses shows the refusal under Name in place of the rule; any other refusal shows under Description. The dialog stays pending until the inventory that follows lists the new skill; it then selects the skill and opens its `SKILL.md` as Edit does (§6 "Edit (detail pane)").

### Refusal under a field

A CLI refusal of what a field holds shows under the field as one paragraph, the message then the hint, in `type.notice` `tone.danger.fg`, as an alert (`src/components/problem-text.tsx`). `src/lib/problem-text.ts` sentence-cases each part and ends it with a full stop, and changes nothing else: commands, paths and the skill names the field knows keep the CLI's spelling. When the CLI's hint names a flag or a command, the field shows a hint in the app's own words that names its own control (`src/lib/app-hint.ts`). The error toast says a refusal the same way.

### Agent client card (Agents page)

One card per agent configuration whose client does not read the library, in a 4-column grid with `agent-card.gap`: `card.bg`, 1px `card.border`, `agent-card.radius`, `agent-card.padding`.

- Top row: the `agent-card.logo` logo on the left, the Enabled switch on the right.
- Then the name in `type.agent-card-name` `card.title`; the configuration path in `type.technical` `text.caption`, one line with an ellipsis; the counts in `type.secondary` `card.text`, numbers in `text.primary` 500.
- Enabled decides only where later installs and creations go when no agent is named. It never changes a placement, so the counts do not move when the switch does.

### Universal card (Agents page)

One full-width card below the grid, the same surface, for every configuration whose client reads the library. A lead column `universal-card.lead-width` wide: the clients' logos stacked (`agent-card.logo`, ringed in `card.chip-ring`), the title in `type.agent-card-name`, a line in `type.secondary` `card.text` and the library path in `type.technical` `text.caption`. Then one column per client behind a 1px `shell.divider` on its left: name, path and counts, the skill count being the library's. No switch: a universal client sees every library skill whatever its setting.

### Plugin card (Plugins)

`card.bg`, `plugin-card.radius`, 1px `card.border`, in 3 CSS columns with `plugin-card.gap`; a card never breaks across columns.

- Top, `plugin-card.padding`: a `plugin-card.tile` tile with the plugin's own icon from the inventory, or the name's first letter on `button.secondary-bg`. The tile is never dimmed; a disabled plugin says so with its pill. On the right the Disabled pill, when the client turned the plugin off, then the version pill.
- Then the name in `type.plugin-name` and the agent and marketplace in `text.muted`.
- Contents under a `card.border` hairline: the groups for skills, servers and hooks, each a caption and `plugin-card.row-height` tree rows with a `plugin.connector` line (the last row's stops at the middle), the group's dot, the name and mono meta (`type.technical-sm`, `text.caption`). A skill or server row has a chevron, opens the skill in Skills or the server in MCP servers, and hovers `plugin.row-hover`; a hook row opens nothing.

### Toast

`toast.offset` from the bottom, centred on the window; `overlay.bg`, `overlay.border`, `toast.radius`, `toast.min-height`, `toast.padding`; text `type.toast` `toast.text`; an optional action in `button.toast-action-text`.

- As wide as its text, up to `toast.max-width`; past it the text wraps and the toast grows taller. A one-line toast keeps `toast.min-height`, so it looks as it did before toasts wrapped.
- The action keeps its width on the right of the text and is centred on the toast's height, wrapped or not: the toast is a pill, so its middle is the line the eye follows, and a short action lined up with one line of two would look misplaced on the rounded end.

- One toast at a time: a new one replaces the one showing.
- It goes after 2.6s, or 5s with an action.
- A toast for a command that runs, with its cancel, stays until the command ends and the toast of its outcome replaces it. Progress changes its text in place.

### Attention toast

A command that exited 0 and logged warnings. The error toast's shape, with the success text in `type.toast` `toast.text` and each warning in `type.notice` `tone.attention.fg`, its action when it has one, and a ghost dismiss. It goes after 5s, and its activity entry is `attention`, not `success`.

### Error toast

A command that fails for a reason the app does not handle itself (`src/store/runner.ts`). The toast's place, `overlay.bg` and `overlay.border`, with `popover.shadow`, `toast.detail-radius`, `toast.detail-width` and `toast.detail-padding`. The CLI's message and hint as one paragraph (§6 "Refusal under a field") in `type.toast` `toast.text`; lines that say more below it, as the CLI wrote them, in `type.notice` `text.muted`; each warning in `type.notice` `tone.attention.fg`. A ghost dismiss on the right. It stays until dismissed when it has a hint, and goes after 5s without one.

### Stale bar

A bar across the top of the main panel while the screens show an inventory that may be out of date (`src/app/stale-bar.tsx`): `stale-bar.height`, `stale-bar.padding`, a 1px bottom border, text `type.notice` `text.secondary`. While serve restarts: `tone.attention` with a `Spinner`. When a scan fails or serve stops for good: `tone.danger`, the reason, and a small ghost retry on the right. Nothing shows while the inventory is fresh.

### Machine label

Under the product name in the sidebar: `type.machine-label`, `text.muted`, one line with an ellipsis.

### Timeline (history)

Grid `timeline.col 1fr`, `timeline.gap`. Dot `timeline.dot`, round: `timeline.dot-install` for an upstream import, `timeline.dot-update` for a commit made with Git, `timeline.dot-yours` for one agentx wrote. The dot is centred in a box as high as the entry's first line (`timeline.line` beside text, `timeline.button-line` beside a small button), with a 1px `timeline.connector` from that box to the next entry. Subject `type.timeline-entry` `text.primary`, meta `type.timeline-meta` `timeline.meta`. When the list is cut short, the last item is a hollow dot (inset ring 1.5px `timeline.more-ring`) and a ghost button that opens the History sheet.

### Menu

A list of actions under a button (`src/components/ui/dropdown-menu.tsx`): `menu.min-width`, `menu.radius`, `overlay.bg` with 1px `overlay.border` and `menu.shadow`, `menu.padding`, `menu.offset` below its trigger and aligned to its end. A row is `menu.row-radius`, `menu.row-hover` when pointed at or focused, with a `type.body` `text.primary` line over a `type.technical-sm` `text.caption` line such as a path. Escape or a click outside closes it. No animation.

### Split button

Two ghost icon buttons that meet with no gap and share one outline, split by a 1px `button.split-divider` on the chevron's left edge: the action (`button.icon-size` square, rounded at its left end) and a narrower chevron that opens the action's menu (rounded at its right end). Only the action shows when there is nothing to choose. Show in Finder on a file row is one.

### Popover (Filter)

`popover.width`, `popover.radius`, `overlay.bg` with 1px `overlay.border` and `popover.shadow`, 14px padding, `popover.gap` between groups, `popover.offset` below its anchor. Each group opens with a caption in `popover.caption`: a row of choice pills (`type.secondary`, count `type.technical-sm` `text.caption`; the chosen one `button.secondary-bg`, `text.primary`), a segmented control, and agent rows of `popover.row-radius` with the round checkbox, a 20px logo and the name. A ghost reset shows while a filter is applied. A click outside or Escape closes it.

### Sheet (History)

`sheet.width`, `sheet.inset` from the top, right and bottom, `sheet.bg` with 1px `sheet.border`, `sheet.radius`, `overlay.shadow`, `sheet.padding`, over `shell.scrim`. Title `type.dialog-title`, the subject's name under it in `type.technical` `text.caption`, a ghost close at the top right; Escape closes it too. Content scrolls down only: a long line wraps, never clips or scrolls sideways. Each entry is a small wrapping ghost button with a chevron that expands it to the files its commit changed, one row each: status and path in `type.technical-sm`, `text.caption` and `text.secondary`.

### Sheet (What's new)

The History sheet's frame at `sheet.width-wide`, for what an update changes before the user takes it. Under the skill's name, the base and newer version in `type.technical-sm` `text.caption`, each by its date when the CLI dates it, else by its short id. Below, the File list (§6) with every row open: status `type.technical-sm` `text.caption` (`accent.danger` for a deleted file), path `type.technical` `file-row.path`, and the patch in the Code pane with the diff tints, its hunks alone, numbered as in the newer version.

- A patch with no lines to show, a read in progress and a failed read each say so in `type.secondary` `text.hint`; a failed read adds the CLI's message in `type.technical-sm` `text.caption` and raises no error toast.
- An update from another machine cannot be diffed before it is taken: the sheet lists its files in the same card, status and path, with no chevron.

### Code pane

`code.bg`, `type.code`, grid `code.gutter 1fr`: line number in `code.line-number`, right-aligned · marker · text. Front matter and keys `code.frontmatter`, headings `code.heading`, list items `code.list`, prose `code.prose`. Diff tints: an added line on `diff.added-bg`, a removed line on `diff.removed-bg` with no number. On a tinted line, front matter takes `code.prose` so it keeps 4.5:1.

### File list (detail pane FILES)

A `card.bg` card, `file-card.radius`, 1px `card.border`, one row per file: `file-row.height`, `file-row.padding-x`, a 1px `file-row.divider` between rows, `file-row.hover` on hover or focus.

- A row: a chevron (turns 90° when open), the path in `type.technical` (`file-row.path`, `file-row.path-open` while open), a `file-row.edited-label` status for a file the user changed, and the meta (line count, binary, or size) in `file-row.meta` at the right. An open row shows the file in the Code pane below it.
- SKILL.md comes first and open; the rest follow by path, a file the user removed among them.
- A file on disk has a Show in Finder split button over the meta, beside the trigger, never inside it. While the row is hovered, focused or has its menu open, the meta fades out and the button fades in at the same spot. The meta stays laid out, so the row never shifts, and the button stays in the tab order throughout. A removed file has no button.
- The button opens the file at the skill's first place: the library when it holds the skill, else the first placement in the row's agent order. The chevron's menu lists every other placement, the path as the inventory carries it. A skill with one place has no chevron.

### Key hint

A key such as `esc` or `⌘K` (`src/components/key-hint.tsx`): `key-hint.padding`, `key-hint.radius`, `button.secondary-bg`, `type.key-hint` `text.muted`.

### Inputs

`input.height`, `input.bg`, `input.border`, `input.radius`, padding 0 `input.padding-x`, `type.input`; focus `input.border-focus`. `type.input-mono` for URLs and commands. List search field: `input.search-height`, `input.search-radius`, `input.search-padding`, icon on the left, placeholder in `input.placeholder`.

### Settings

The Settings page (`src/screens/settings`): the serif page title, then one section per area, a `shell.divider` hairline between sections and none above the first.

- A section is a `settings.columns` grid with `settings.column-gap` and `settings.section-padding` above and below. The label column holds the label in `type.settings-label` `text.primary` and one line under it in `type.secondary` `text.muted`; the content column holds the rows, `settings.section-gap` apart.
- Inputs are `input.height`. Paths and ids are `type.technical`, ids in `text.hint`.
- A switch row is the setting's name in `type.body` `text.primary` over its line in `type.secondary` `text.muted`, the switch on the right (§6 "Switch").
- A failure under a field is a refusal under a field (§6 "Refusal under a field").
- Editor holds an app preference, not a machine setting (ADR 0013): it runs no command and saves the moment it changes. A radio group (§6 "Radio group") lists the editors found on this machine, and one chosen before that is gone since, so the choice still shows. A custom command is a mono input under the group, saved on Enter or when it loses focus, with a hint under it in `type.secondary` `text.hint`.
- A choice that replaces something (an existing export file, the machine's settings on import) opens a confirm dialog with a danger confirm; a picked file's path shows in `type.technical` `text.caption`.
- The import result is one `type.secondary` line per outcome: what it installed or updated in `text.secondary`, then the warnings in `tone.attention.fg` after an `AlertTriangleIcon` (`xs`). A skipped skill's name has a dotted underline and the CLI's reason as its tooltip. When nothing changed, the one line is `text.muted`.

### Publish status (title bar)

At the right end of the title bar (`src/app/publish-status.tsx`, drawn by `src/components/status-button.tsx`): a pill `shell.status-height` high, `shell.status-radius`, transparent, hover `shell.topbar-hover` with the label in `text.secondary`. An 8px dot (`shell.status-dot-radius`) in a 16px box, then the label in `type.status` `shell.topbar-text`.

- Three states, from the inventory's library and the Sources slice: no account remote (dot `text.hint`; goes to Settings), skills to publish (`shell.status-pending` with `shell.status-pending-halo`; publishes them all), and everything published (`shell.status-published` with `shell.status-published-halo`; checks for updates).
- It counts your own skills edited since they were published, with the predicate the Skills screen's banner uses (`hasEditsToPublish` in `src/screens/skills/banners.ts`), so the title bar and the Skills screen never disagree. A skill never published is not counted: a bare publish leaves it out, and its own banner publishes it.
- While the publish runs, and until the inventory that follows arrives, the label counts its progress and the pill is `aria-busy`. While the check runs, a `Spinner` takes the dot's place; it is the Skills screen's check, so only one runs at a time.
- Hidden while the startup layer shows, and until the inventory and the sources are read, so it never guesses a state.

### Command palette

The ⌘K palette (`src/app/command-palette.tsx`): ACTIONS, GO TO, SKILLS, in that order, each under its caption. A row is its icon in `palette.icon`, its label in `type.body` and an optional hint in `type.palette-hint`; typing keeps the rows whose label holds the text.

- ACTIONS shows an action only when it has something to act on: Update all while an update is offered, Publish all while the publish status counts skills to publish.
- SKILLS lists every skill in the Skills list's order, its origin as the hint; while nothing is typed, only the first four, so the actions and screens stay in view.

## 7. Iconography

Stroke icons on a 24 grid, `stroke-width` 1.75–2.25, round caps and joins. Fill only for the dot glyph. No icon fonts, no emoji.

An icon is a hand-written React component that takes the text colour; an illustration (such as the agentx mark) is an SVG file in `src/assets` shown as `<img>`.

Sizes: `xs` 14px, `sm` 15px, `md` 16px (the default), `lg` 18px.

The product mark is `shell.logo-mark` on `shell.logo-bg` with a 1px `shell.logo-ring` ring: 30px with `shell.logo-radius` in the sidebar (the height of the product name and machine label beside it), 40px with `startup.logo-radius` on the startup layer. Its SVG is also the source of the app icons: after changing it, run `pnpm tauri icon src/assets/agentx-mark.svg` and keep only the files `src-tauri/tauri.conf.json` lists. The SVG holds the two colours as hex values (`lime-400`, `gray-990`); change them together with the tokens.

## 8. Motion

Nothing loops forever, and nothing waits on purpose.

- Hover and background changes: no transition, or at most `.18s`.
- Switches: `.18s` background.
- Chevrons: `rotate(90deg)` in `.15s`.
- File row: the meta and the Show in Finder button swap by opacity in `.15s`.
- Floating bar (`animate-floating-bar`): `translateY(24px) → 0` and opacity 0 → 1, `.22s ease`, when it appears.
- Check updates and Check tools icons (`animate-turn-once`): one full rotation, 1s ease, while the command runs.
- Startup check running (`animate-check-spin`): the status ring turns once a second, linear, until the check settles.
- Screens fade in (`animate-fade-in`): opacity 0 → 1 over `.2s ease-out` when they mount, once the startup checks pass; a passing launch shows no startup layer.
- A launch whose checks run over 2s shows a `Spinner` and a status line (`type.secondary`, `text.muted`) in the empty panel, faded in the same way, each status for at least 1.5s. From 5s into a check, a countdown line under it (`type.row-desc`, `text.hint`).
- Diff pane choice: `opacity .2s, box-shadow .2s`.
- Primary button enable and disable: `opacity .2s`.
