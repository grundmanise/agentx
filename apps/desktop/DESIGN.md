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

A dark, quiet desktop tool. One accent (lime) carries the accent and action signals; everything else is near-black and grey. Serif for page titles only; a mono face for anything technical (paths, ids, dates, captions). Surfaces are flat: elevation comes from one shade step and, on floating layers, a soft black shadow. No gradients, no emoji, no icon-per-row decoration. The page declares `color-scheme: dark`, so the system draws native controls such as scrollbars for a dark background; the app never styles a scrollbar.

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

Monogram tints, one hue each (§6 "Agent client logo chip, monogram"): a dark tint (`-900`, HSL lightness 22%, saturation 40%) behind a light letter (`-200`, lightness 80%, saturation 75%), ten hues spread round the colour wheel.

| Hue      | Dark                   | Light                  |
| -------- | ---------------------- | ---------------------- |
| `red`    | `red-900` `#4f2222`    | `red-200` `#f2a6a6`    |
| `orange` | `orange-900` `#4f3422` | `orange-200` `#f2c4a6` |
| `amber`  | `amber-900` `#4f4122`  | `amber-200` `#f2dba6`  |
| `lime`   | `lime-900` `#414f22`   | `lime-200` `#dbf2a6`   |
| `green`  | `green-900` `#224f31`  | `green-200` `#a6f2bf`  |
| `teal`   | `teal-900` `#224f49`   | `teal-200` `#a6f2e8`   |
| `sky`    | `sky-900` `#22414f`    | `sky-200` `#a6dbf2`    |
| `blue`   | `blue-900` `#22314f`   | `blue-200` `#a6bff2`   |
| `violet` | `violet-900` `#31224f` | `violet-200` `#bfa6f2` |
| `pink`   | `pink-900` `#4f2240`   | `pink-200` `#f2a6d9`   |

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
| `accent.yours` | `violet-400` | Hub skills, your side of a diff, publish |
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
| `text.faint` | `gray-700` | Line numbers |

#### Window and shell

| Token | Primitive | Element |
| --- | --- | --- |
| `shell.bg` | `gray-990` | Window body, sidebar, startup layer |
| `shell.content-bg` | `gray-970` | Content area, both split columns, command field |
| `shell.loading-veil` | `gray-970` at 40% | The frosted pane over the kept inventory while the startup checks run (§8) |
| `shell.divider` | `gray-890` | Split column divider, section dividers, table header rule |
| `shell.scrim` | `black/50` | Behind the history sheet |
| `shell.scrim-strong` | `black/55` | Behind dialogs and the palette |
| `shell.nav-text` | `gray-300` | Inactive nav item; `text.primary` on hover |
| `shell.nav-active-bg` | `gray-910` | Active nav item |
| `shell.nav-active-text` | `gray-50` | Active nav item label |
| `shell.nav-tree` | `gray-800` | 1px rule to the left of the nav items nested under another |
| `shell.nav-badge-bg` | `accent.primary` | Inbox count badge (digits `on-accent`) |
| `shell.topbar-text` | `gray-300` | Command field placeholder and icon (`text.secondary` on hover), hub status label |
| `shell.status-published` | `accent.primary` | Hub status dot, nothing to publish (halo `lime-400/15`) |
| `shell.status-pending` | `accent.yours` | Hub status dot, skills to publish (halo `violet-400/18`) |
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
| `button.ghost-bg` | `gray-50/3.5` | Ghost and icon button at rest, sort toggle, publish status |
| `button.ghost-hover` | `gray-50/7.5` | Ghost / icon button hover |
| `button.danger-text` | `accent.danger` | Danger ghost (Remove, Cancel, Restore base version) |
| `button.danger-bg` | `red-400/10` | Danger button at rest |
| `button.danger-hover` | `red-400/17` |  |
| `source-pill.bg` | `gray-910` | Source pill on Sources, inactive (label `text.secondary`) |
| `source-pill.active-bg` | `gray-50` | Source pill on Sources, active |
| `source-pill.active-text` | `on-accent` |  |
| `source-pill.count` | `gray-400` | Skill count after the name of an inactive source pill: 4.8:1 on `source-pill.bg` |
| `source-pill.active-count` | `gray-600` | Skill count on the active source pill: 7.5:1 on `source-pill.active-bg`, the one use of `gray-600` as text, on a light fill |

#### Status pills

| Token | Primitive | Element |
| --- | --- | --- |
| `pill.update-bg` | `accent.primary` | Update pill |
| `pill.update-text` | `on-accent` |  |
| `pill.update-hover` | `accent.primary-hover` | The sidebar's update pill under the pointer |
| `pill.neutral-bg` | `gray-860` | Not-published pill, fork pill, Read-only |
| `pill.neutral-hover` | `gray-800` |  |
| `pill.neutral-text` | `gray-200` |  |
| `pill.removed-bg` | `red-400/12` | Removed-from-source pill |
| `pill.removed-text` | `accent.danger` |  |
| `pill.disabled-bg` | `amber-400/10` | Disabled plugin pill |
| `pill.disabled-text` | `accent.attention` |  |
| `pill.version-bg` | `gray-880` | Plugin version |
| `pill.version-text` | `gray-300` |  |
| `pill.universal-bg` | `gray-850` | Universal pill on a universal agent's row in the agent picker |
| `pill.universal-text` | `gray-250` | 5.8:1 on `pill.universal-bg` |

#### Inputs, switches, checkboxes

| Token | Primitive | Element |
| --- | --- | --- |
| `input.bg` | `gray-980` | Text inputs, search fields |
| `input.border` | `gray-860` |  |
| `input.border-focus` | `gray-700` |  |
| `input.border-invalid` | `accent.danger` | A field marked `aria-invalid`, focused or not |
| `input.text` | `gray-50` |  |
| `input.placeholder` | `gray-500` | Placeholder text and leading icon |
| `switch.on-track` | `accent.primary` | Switch on (full and compact) |
| `switch.on-knob` | `on-accent` |  |
| `switch.off-track` | `gray-850` | Switch off |
| `switch.off-knob` | `gray-400` |  |
| `checkbox.ring` | `gray-700` | Unchecked popover checkbox |
| `checkbox.checked-bg` | `accent.primary` |  |
| `checkbox.check` | `on-accent` |  |

#### Segmented control (Update review, filter State, agent client placement)

| Token | Primitive | Element |
| --- | --- | --- |
| `segmented.track-bg` | `gray-950` | Track |
| `segmented.card-track-bg` | `gray-980` | Track on a card (placement on an agent client card) |
| `segmented.track-border` | `gray-870` |  |
| `segmented.text` | `gray-50` | Unselected segment; `gray-400` once another segment is chosen |
| `segmented.yours-selected` | `accent.yours` | Your side chosen (label `on-accent`) |
| `segmented.theirs-selected` | `accent.theirs` | Base version or update side chosen |
| `segmented.both-selected` | `gray-50` | Both sides kept |
| `segmented.neutral-selected` | `gray-850` | Filter State segments (label `text.primary`) |
| `tabs.list-bg` | `gray-910` | Track behind the tabs |
| `tabs.text` | `gray-300` | A tab that is not active; `text.primary` on hover |
| `tabs.active-bg` | `gray-850` | The active tab |
| `tabs.active-border` | `gray-800` | 1px edge of the active tab |
| `tabs.active-text` | `gray-50` | The active tab's label |

#### Filter row (Skills toolbar)

| Token | Primitive | Element |
| --- | --- | --- |
| `filter.bg` | `gray-880` | Filter button at rest |
| `filter.text` | `gray-200` | Matches `filter.sort-text` |
| `filter.active-bg` | `lime-400/12` | Filter button with any filter applied |
| `filter.active-text` | `accent.primary` |  |
| `filter.badge-bg` | `accent.primary` | Count badge (digits `on-accent`) |
| `filter.chip-bg` | `gray-920` | Applied agent client chip |
| `filter.chip-text` | `gray-150` |  |
| `filter.chip-hover` | `gray-880` |  |
| `filter.sort-text` | `gray-200` | Sort toggle (A-Z, Z-A) |

#### Lists and rows

| Token | Primitive | Element |
| --- | --- | --- |
| `row.hover` | `gray-920` | Skills row hover |
| `row.selected-bg` | `gray-900` | Selected Skills row |
| `row.selected-bar` | `accent.primary` | 2px bar on the selected row |
| `row.name` | `gray-50` | Row name, enabled |
| `row.desc` | `gray-300` | Description line |
| `row.chip-ring` | `shell.content-bg` | 2px ring behind overlapping agent client logos in rows |
| `group.label` | `gray-300` | Source group caption |
| `group.count` | `gray-500` | Count beside the caption |
| `group.divider-label` | `gray-500` | Caption of the sub-group of skills in no agent client |
| `subgroup.label` | `gray-400` | Caption of a section toggle: a plugin's subgroup under PLUGINS, a source in the Update skills dialog |
| `subgroup.count` | `gray-500` | Count beside a plugin's caption |
| `subgroup.source` | `gray-600` | The marketplace at the right end of a plugin's header. Below 4.5:1 on purpose: it is a secondary hint, and the plugin's name and count stay the first things read |
| `list.guide` | `gray-700` | 1px guide line down the left of what a list's section toggle holds, under its chevron (§6 "Group header (lists)") |
| `subgroup.hover` | `gray-920` | A section toggle under the pointer: a plugin's header, a source's header in the Update skills dialog |
| `table.row-hover` | `gray-930` | Hover on Inbox update rows, server rows |
| `table.header-text` | `gray-500` | Column captions |
| `add-skill.row-hover` | `gray-880` | Hover on a skill row of the Add skill dialog |
| `update-skills.row-hover` | `gray-880` | Hover on a skill row of the Update skills dialog, before its run |

#### Skill glyph

| Token | Primitive | Element |
| --- | --- | --- |
| `glyph.idle` | `gray-400` | Dot or fork glyph: no action to take |
| `glyph.update` | `accent.primary` | Dot or fork glyph of a skill with an update to apply |
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
| `monogram.0-bg` | `red-900` | Monogram tint, picked by a hash of the client name |
| `monogram.0-text` | `red-200` | Monogram letters |
| `monogram.1-bg` | `orange-900` |  |
| `monogram.1-text` | `orange-200` |  |
| `monogram.2-bg` | `amber-900` |  |
| `monogram.2-text` | `amber-200` |  |
| `monogram.3-bg` | `lime-900` |  |
| `monogram.3-text` | `lime-200` |  |
| `monogram.4-bg` | `green-900` |  |
| `monogram.4-text` | `green-200` |  |
| `monogram.5-bg` | `teal-900` |  |
| `monogram.5-text` | `teal-200` |  |
| `monogram.6-bg` | `sky-900` |  |
| `monogram.6-text` | `sky-200` |  |
| `monogram.7-bg` | `blue-900` |  |
| `monogram.7-text` | `blue-200` |  |
| `monogram.8-bg` | `violet-900` |  |
| `monogram.8-text` | `violet-200` |  |
| `monogram.9-bg` | `pink-900` |  |
| `monogram.9-text` | `pink-200` |  |
| `agent-picker.badge-bg` | `accent.primary` | Count of chosen targets the Add skills bar hides, on its "+n" button (digits `on-accent`) |
| `agent-picker.row-hover` | `gray-880` | Row hover in the agent picker |

#### Cards and banners

| Token | Primitive | Element |
| --- | --- | --- |
| `card.bg` | `gray-950` | Inbox card, agent client card, file-tree card, expanded review file card, a skill's card on the Sources list |
| `card.border` | `gray-870` |  |
| `card.title` | `gray-50` |  |
| `card.text` | `gray-300` |  |
| `card.tag` | `gray-500` | Mono tag beside a card title |
| `card.subrow-hover` | `gray-930` | Sub-rows inside the Updates card |
| `add-card.hover-border` | `gray-700` | Border of a skill's card on the Sources list while hovered (the card is `card.bg` with a `card.border`) |
| `add-card.selected-bg` | `lime-400/6` | A selected skill's card |
| `add-card.selected-border` | `lime-400/50` |  |
| `add-card.check-bg` | `accent.primary` | Fill of the round check of a selected skill (check `on-accent`) |
| `add-card.status-added` | `accent.primary` | Skill status: already added |
| `add-card.status-forked` | `accent.yours` | Skill status: forked |
| `add-card.status-taken` | `gray-400` | Skill status: name taken |
| `add-card.status-remove` | `accent.danger` | Remove, in place of Added while hovered |
| `add-card.check-ring` | `gray-700` | Ring of the round check of a skill that is not selected |
| `source-card.bg` | `gray-950` | Card of the open source on Sources |
| `source-card.border` | `gray-870` |  |
| `source-card.divider` | `gray-870` | Rule above the facts strip, and between its facts |
| `source-card.tile-bg` | `gray-880` | Owner tile |
| `source-card.tile-text` | `gray-200` | Owner's initial on the tile |
| `source-card.name` | `gray-50` | Source name |
| `source-card.fact-label` | `gray-400` | Fact label (Skills, Branch, Commit, Fetched) |
| `source-card.fact-value` | `gray-50` | Fact value |
| `banner.title` | `gray-50` | Banner title (bg/border/tile from `tone.*`) |
| `banner.text` | `gray-300` |  |
| `card.chip-ring` | `card.bg` | 2px ring behind overlapping agent client logos on a card (the Universal card) |

#### Overlays (dialog, popover, palette, sheet, toast, floating bar)

| Token | Primitive | Element |
| --- | --- | --- |
| `overlay.bg` | `gray-940` | Dialog, popover, palette, toast, floating bar, tooltip |
| `floating-bar.rim` | `accent.primary` | The light on the floating bar's rim while an install runs (§8 "Progress rim"), read by its shader |
| `floating-bar.rim-flow` | `teal-200` | The colour that drifts through that light |
| `overlay.border` | `gray-800` |  |
| `overlay.chip-ring` | `overlay.bg` | 2px ring behind overlapping agent client logos in a menu |
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
| `sheet.handle` | `gray-700` | A sheet's handle on hover and while it drags |
| `toast.text` | `gray-50` | A plain toast's text |
| `toast.description` | `gray-300` | The lines under a toast's message, tinted or not |
| `toast.success-bg` | `accent.primary` 9% over `overlay.bg` | Success toast fill, solid |
| `toast.success-border` | `accent.primary` 28% over `overlay.bg` |  |
| `toast.success-text` | `tone.primary.fg` | Success toast message and icon |
| `toast.error-bg` | `accent.danger` 9% over `overlay.bg` | Error toast fill, solid |
| `toast.error-border` | `accent.danger` 28% over `overlay.bg` |  |
| `toast.error-text` | `tone.danger.fg` | Error toast message and icon |
| `toast.warning-bg` | `accent.attention` 9% over `overlay.bg` | Attention toast fill, solid |
| `toast.warning-border` | `accent.attention` 28% over `overlay.bg` |  |
| `toast.warning-text` | `tone.attention.fg` | Attention toast message and icon |

#### Code, file tree and diffs

| Token | Primitive | Element |
| --- | --- | --- |
| `code.bg` | `gray-980` | Code pane, hunk block, the activity log's block of lines |
| `code.border` | `gray-890` | Hunk block border, output block border |
| `code.line-number` | `gray-400` | Line numbers: 5.3:1 on `code.bg`, 4:1 on the added tint, where the marker carries the meaning |
| `code.frontmatter` | `gray-400` | Front-matter and keys |
| `code.heading` | `gray-50` | Markdown headings |
| `code.list` | `gray-150` | List items |
| `code.prose` | `gray-250` | Prose |
| `code.strong` | `gray-100` | Code in diff panes |
| `code.context` | `gray-500` | Context lines around a change |
| `markdown.code-bg` | `gray-850` | A code span in inline Markdown, such as a skill's description |
| `markdown.code-text` | `gray-200` | Its text: 7.3:1 on `markdown.code-bg` |
| `markdown.strong` | `gray-150` | Strong text in inline Markdown, brighter than the muted text around it |
| `file-row.hover` | `gray-920` | File tree row hover |
| `file-row.divider` | `gray-890` |  |
| `file-row.path` | `gray-200` | Path |
| `file-row.path-open` | `gray-50` | Path of the file whose pane is open |
| `file-row.meta` | `gray-500` | Line count or size at the row's end |
| `file-row.edited-label` | `accent.attention` | Status of a file the user changed |
| `diff.added-bg` | `lime-400/12` | Added line tint (marker `accent.primary`) |
| `diff.removed-bg` | `red-400/8` | Removed line tint (marker `accent.danger`) |
| `diff.added-word` | `lime-400/24` | Changed text of an added line, over its tint (text `code.strong`: 5.2:1) |
| `diff.removed-word` | `red-400/24` | Changed text of a removed line, over its tint (text `code.strong`: 8.3:1) |
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
| `timeline.connector` | `gray-800` | 1px line between the chevrons of the History sheet |
| `timeline.meta` | `gray-500` | Relative time under an entry |

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
| `type.skill-name` | sans | 30 | 600 |  | -0.035em |  | Detail pane skill name, selectable |
| `type.body` | sans | 14 | 400 |  |  |  | Body, palette rows |
| `type.input` | sans | 14 | 400 |  |  |  | Text inputs and search fields |
| `type.input-mono` | mono | 14 | 400 |  |  |  | Inputs that take a URL or a command |
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
| `type.markdown-code` | mono | 0.9em |  |  |  |  | A code span in inline Markdown, sized to the text around it |
| `type.brand` | sans | 14 | 600 |  | -0.01em |  | Product name in the sidebar |
| `type.machine-label` | sans | 12 | 400 |  |  |  | Machine label under the product name (`text.muted`) |
| `type.nav` | sans | 14 | 500 |  |  |  | Nav item |
| `type.command-field` | sans | 13 | 400 |  |  |  | Command field placeholder |
| `type.key-hint` | mono | 11 | 400 |  |  |  | Key hint (`esc`, `⌘K`) |
| `type.status` | sans | 13 | 500 |  |  |  | Publish status label in the title bar (`shell.topbar-text`) |
| `type.palette-input` | sans | 17 | 400 |  |  |  | Palette search input |
| `type.palette-hint` | mono | 12 | 400 |  |  |  | Hint at the end of a palette row, keys in the palette footer |
| `type.palette-footer` | sans | 12 | 400 |  |  |  | Palette footer (`text.hint`) |
| `type.detail-desc` | sans | 14.5 | 400 | 1.5 |  |  | Detail pane description (`text.muted`, `text-wrap:pretty`), selectable |
| `type.banner-title` | sans | 14 | 500 |  |  |  | Banner title (`banner.title`); its text is `type.secondary` in `banner.text` |
| `type.agent-chip` | sans | 13 | 500 |  |  |  | Agent client name in a toggle chip |
| `type.source-name` | sans | 16 | 500 |  |  |  | Source name on the card of the open source (`source-card.name`) |
| `type.agent-card-name` | sans | 16 | 600 |  |  |  | Agent client name on an Agents page card, and the Universal card's title |
| `type.timeline-entry` | sans | 13.5 | 400 | 20px |  |  | Timeline entry subject (`text.primary`); its line height is `timeline.line`, so the chevron beside it sits on the first line |
| `type.timeline-meta` | sans | 12 | 400 |  |  |  | Timeline entry meta (`text.caption`) |
| `type.notice` | sans | 13 | 400 | 1.5 |  |  | Stale bar text |
| `type.server-name` | sans | 14.5 | 500 |  |  |  | Server name in a server row (`text.primary`) |
| `type.transport-pill` | mono | 11.5 | 500 |  |  |  | Transport pill (`server.transport-text` on `server.transport-bg`) |
| `type.settings-label` | sans | 14 | 500 |  |  |  | Settings section label (`text.primary`); the line under it is `type.secondary` in `text.muted` |
| `type.plugin-name` | sans | 30 | 600 |  | -0.035em |  | Detail pane plugin name, selectable |
| `type.machine-name` | sans | 30 | 600 |  | -0.035em |  | Detail pane machine label, selectable |
| `type.plugin-tile` | sans | 17 | 600 |  | -0.01em |  | The letter of a plugin's monogram tile |
| `type.tab` | sans | 14 | 500 |  |  |  | Tab labels |
| `type.version-pill` | mono | 12 | 500 |  |  |  | A plugin's version pill |

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
| `shell.status-height` | 30 | `h-status` | Hub status pill |
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
| `split.list-width` | 420 | `w-split-list` | Skills left column |
| `split.header-padding` | 30 28 0 | `pt-split-header-top px-split-header pb-0` | Skills list header |
| `split.toolbar-padding` | 20 28 12 | `pt-split-toolbar-top px-split-toolbar pb-split-toolbar-bottom` | Search + filter row block |
| `split.toolbar-gap` | 12 | `gap-split-toolbar` | Search → filter row |
| `split.filter-gap` | 6 | `gap-split-filter` | Filter button → chip → sort |
| `split.list-padding` | 0 28 24 14 | `pt-0 pr-split-list-right pb-split-list-bottom pl-split-list-left` | Scrolling list |
| `split.list-top` | 20 | `pt-split-list-top` | Above the rows of a split list with no search and filter block over it (Machines) |
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
| `row.spacing` | 2 | `gap-rows`, `pt-rows` | Between rows; `pt-rows` on a row of the Skills list, which places each row itself |
| `row.glyph-col` | 18 | `w-row-glyph` | Glyph column |
| `row.off` | 50% | `opacity-row-off` | Opacity of a row in no agent client, and of a disabled plugin's row on Plugins: washed out |
| `group.padding` | 4 14 8 | `pt-group-top px-group pb-group-bottom` | Group header |
| `group.spacing` | 16 | `pt-group-spacing` | Above each group but the first, outside its pinned header |
| `group.header-height` | 30 | `min-h-group-header`, `top-group-header` | Least height of a group header; a plugin's header pins this far down, under its group's |
| `group.gap` | 8 | `gap-group` | Label → count |
| `subgroup.padding` | 6 14 | `py-subgroup-y px-subgroup-x` | A section toggle: a plugin's header under PLUGINS, a source's header in the Update skills dialog |
| `subgroup.spacing` | 8 | `pt-subgroup-top` | Above a plugin's header, but the first in its group |
| `subgroup.indent` | 28 | `pl-subgroup-indent` | Left inset of one level of nesting under a section toggle: a group's rows and plugin headers in the Skills list, clear of the group's guide line, and a source's rows in the Update skills dialog |
| `subgroup.nested` | 56 | `pl-subgroup-nested` | Left inset of two levels of nesting: a plugin's rows, whose glyph sits under the plugin's name |
| `list.guide-inset` | 20 | `left-list-guide` | The guide line's offset from the list's left edge, under a group's chevron |
| `list.guide-nested-inset` | 48 | `left-list-guide-nested` | The second guide line's offset, under a plugin's chevron |
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
| `tabs.list-height` | 36 | `h-tabs-list` | Track of the tabs |
| `tabs.list-padding` | 3 | `p-tabs-list` | Track around the tabs |
| `tabs.gap` | 12 | `gap-tabs` | Tabs → the active tab's content |
| `filter.badge` | 16 | `size-filter-badge` | Count badge |
| `agent-picker.badge` | 16 | `size-agent-picker-badge` | Count badge on the Add skills bar's "+n" button |
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
| `add-search.height` | 40 | `h-add-search` | Search field of the Sources screen, a pill across the page |

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
| `banner.text-width` | 240 | `min-w-banner-text basis-banner-text` | The narrowest an alert banner's text gets; narrower, its buttons wrap under it |
| `banner.tile` | 32–34 |  |  |
| `banner.text-gap` | 3 | `gap-banner-text` | Title → text |
| `agent-card.padding` | 22 | `p-agent-card` | Agents page cards |
| `agent-card.gap` | 12 | `gap-agent-card` | Grid gap |
| `agent-card.logo` | 44 | `size-agent-card-logo` |  |
| `universal-card.lead-width` | 260 | `w-universal-lead` | Lead column of the Universal card |
| `universal-card.column-min-width` | 220 | `grid-cols-universal` | Narrowest client column of the Universal card |
| `plugin.tile` | 40 | `size-plugin-tile` | Plugin icon or monogram tile in the detail pane |
| `plugin.row-tile` | 28 | `size-plugin-row-tile` | Plugin icon or monogram tile in a Plugins list row |
| `detail-facts.gap` | 12 40 | `gap-y-detail-facts-y gap-x-detail-facts` | Between the facts under a plugin's or a machine's name (row, column) |
| `plugin.name-col` | 200 | `w-plugin-name-col` | The name column of a plugin's Skills, MCP servers and Hooks tables |
| `add-card.spacing` | 6 | `pt-add-card` | Between the skills' cards of the Sources list: on each card after the first of its source, which the list places itself |

### Overlays

| Token | px | Class | Element |
| --- | --- | --- | --- |
| `dialog.width` | 480 | `w-dialog` |  |
| `dialog.padding` | 32 | `p-dialog` |  |
| `dialog.gap` | 22 | `gap-dialog` | Between dialog blocks |
| `dialog.window-inset` | 32 | none: part of `dialog.max-height` | The least gap between a dialog and the window's top and bottom |
| `dialog.max-height` | the window's height, less `dialog.window-inset` above and below | `max-h-dialog` | A block of the dialog that scrolls, such as the Remove dialog's agents, gives up the room; the rest stays |
| `popover.width` | 360 | `w-popover`, `min-w-popover` | Filter popover: room for three choice pills in a row; the least width of a popover that fits its content |
| `popover.padding` | 8–14 |  | 8 for row lists, 14 for grouped content |
| `popover.gap` | 16 | `gap-popover` | Between groups |
| `popover.row-height` | 32–34 |  |  |
| `popover.offset` | 8 | none: a number passed to the popover's `sideOffset` | Gap from its anchor |
| `popover.window-inset` | 10 | none: a number passed to the popover's, hover card's and menu's `collisionPadding` | The least gap between the popover, hover card or menu and the window's edges |
| `popover.max-height` | the room from its anchor to the window's edge, less `popover.window-inset` | `max-h-popover` | Radix measures it, so it is a utility in `tokens.css` with no theme value; the popover scrolls past it |
| `popover.max-width` | the room the window has across, less `popover.window-inset` on each side | `max-w-popover` | Radix measures it, so it is a utility in `tokens.css` with no theme value; a popover that fits its content (`width="fit"`, the sync popover) widens up to it |
| `popover.origin` | the point of the popover nearest its anchor | `origin-popover` | Radix measures it, so it is a utility in `tokens.css` with no theme value; the popover zooms in and out from it |
| `menu.min-width` | 240 | `min-w-menu` | Menu |
| `menu.max-height` | the room from its trigger to the window's edge, less `popover.window-inset` | `max-h-menu` | Radix measures it, so it is a utility in `tokens.css` with no theme value; the menu scrolls past it |
| `menu.max-width` | the window's width, less `popover.window-inset` on each side | `max-w-menu` | Radix measures it, so it is a utility in `tokens.css` with no theme value; a long path wraps |
| `menu.padding` | 6 | `p-menu` | Around the menu's rows |
| `menu.offset` | 4 | none: a number passed to the menu's `sideOffset` | Gap from its trigger |
| `tooltip.offset` | 6 | none: a number passed to the tooltip's `sideOffset` | Gap from its trigger |
| `hover-card.max-height` | the room from its trigger to the window's edge, less `popover.window-inset` | `max-h-hover-card` | Radix measures it, so it is a utility in `tokens.css` with no theme value; the hover card scrolls past it |
| `tooltip.text-max-width` | 280 | `max-w-tooltip-text` | A tooltip that explains a term (`size="text"`); past it the sentence wraps |
| `sheet.width` | the width the user dragged the sheet to, else 1120 | `w-sheet-dragged` | Every sheet: History, What's new and Activity log. The drag sets the variable on the sheet, so it is a utility in `tokens.css` that falls back to the theme value `--width-sheet` |
| `sheet.min-width` | 420 | `min-w-sheet` | The narrowest a sheet drags to |
| `sheet.max-width` | 90% of the window | `max-w-sheet` | The widest a sheet drags to, and opens at in a narrow window |
| `sheet.handle-width` | 8 | `w-sheet-handle` | The drag handle on a sheet's left edge |
| `sheet.inset` | 10 | `top-sheet right-sheet bottom-sheet` | From top/right/bottom |
| `sheet.padding` | 28 32 | `py-sheet-y px-sheet` |  |
| `palette.width` | 640 | `w-palette` |  |
| `palette.input-height` | 60 | `h-palette-input` |  |
| `palette.top` | 112 | `top-palette` | From the window top |
| `palette.list-max-height` | 420 | `max-h-palette-list` | Palette result list |
| `adopt.list-max-height` | 280 | `max-h-adopt-list` | The adopt dialog's list of skills; it scrolls past it |
| `agent-picker.list-max-height` | 320 | `max-h-agent-picker-list` | The agent picker's list; it scrolls past it |
| `add-skill.list-max-height` | 264 | `max-h-add-skill-list` | The Add skill dialog's list of skills found; it scrolls past it |
| `update-skills.list-max-height` | 400 | `max-h-update-skills-list` | The Update skills dialog's list of skills; it scrolls past it |
| `filter-tags.width` | 264 | `w-filter-tags` | The agent filter chip's panel of tags: from the chip to the sort toggle's right edge |
| `filter-tags.max-height` | 112 | `max-h-filter-tags` | The tags in that panel: three rows and half of a fourth, cut through its names at the panel's bottom edge; they scroll past it |
| `agent-list.max-height` | 240 | `max-h-agent-list` | The list of agents in an agent list card, running to the card's top and bottom edges; it scrolls past it |
| `key-hint.padding` | 3 8 | `py-key-hint-y px-key-hint` | Key hint (`esc`, `⌘K`) |
| `key-hint.gap` | 2 | `gap-key-hint` | Between the keys of a combination (`⌘` `K`) |
| `toast.offset` | 24 | none: `--inset-toast`, passed to Sonner's `offset` in `Toaster` (`src/components/ui/sonner.tsx`) | Below the title bar (`shell.titlebar-height` + `toast.offset` from the top edge) and from the right edge |
| `floating-bar.offset` | 24 | `bottom-floating-bar` | From the bottom edge |
| `floating-bar.height` | 56 | `h-floating-bar` |  |

### Code, diff, timeline, startup

| Token | px | Class | Element |
| --- | --- | --- | --- |
| `code.padding-y` | 14 | `py-code` | Pane top/bottom |
| `code.gutter` | 48 12 | `grid-cols-code` (with the text column) | Line-number and marker columns |
| `code.gap` | 12 | `gap-x-code` | Between gutter and text |
| `code.max-height` | 340 | `max-h-code` | The pane scrolls past it |
| `code.min-height` | 200 | none: part of `file.open-min-height` | The least height a long patch's pane in What's new gives up to |
| `file.open-min-height` | `file-row.height` + `code.min-height` + 2 | `min-h-file-open` | An open file's block in What's new: its row, its pane and its borders |
| `code.height` | 340 | `h-code` | The SKILL.md pane while the files are read, at the pane's full height |
| `code.loading-blur` | 8 | `blur-code-loading` | The stand-in SKILL.md under the spinner while the files are read |
| `shell.panel-loading-blur` | 12 | `backdrop-blur-panel-loading` | The frosted pane over the kept inventory while the startup checks run (§8) |
| `file-row.height` | 44 | `h-file-row` | File tree row |
| `file-row.padding-x` | 16 | `px-file-row`, `right-file-row` for the control at the row's end |  |
| `hunk.gap` | 12 | `gap-hunk` | Between hunk blocks |
| `hunk.header-padding` | 8 8 8 16 | `py-hunk-header pr-hunk-header pl-hunk-header-left` |  |
| `hunk.pane-margin` | 4 8 | `my-hunk-pane-y mx-hunk-pane` | Around the two panes |
| `diff.segment` | 56 4 | `w-diff-segment h-diff-segment` | Progress segment w h |
| `diff.segment-gap` | 4 | `gap-diff-segment` |  |
| `timeline.line` | 20 | `h-timeline-line` | The box the chevron is centred in: the line height of `type.timeline-entry` |
| `timeline.indent` | 22 | `pl-timeline-indent` | The meta line and files of a History sheet entry, past its chevron: the `xs` icon (14) plus `button.icon-gap` (8) |
| `timeline.entry-gap` | 16–20 |  | Bottom padding per entry |
| `log.time-width` | 8ch | `w-log-time` | Time column of an activity log line, which a line of output leaves empty |
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
| `tabs.list-radius` | 10 | Track of the tabs |
| `tabs.trigger-radius` | 8 | A tab |
| `agent-chip.radius` | 9999 | Agent client toggle chip |
| `logo.radius` | 50% | Agent client logo |
| `agent-picker.row-radius` | 10 | Row of the agent picker |
| `add-skill.row-radius` | 10 | Skill row of the Add skill dialog |
| `update-skills.row-radius` | 10 | Skill row of the Update skills dialog |
| `shell.nav-radius` | 10 | Nav item |
| `shell.logo-radius` | 8 | Sidebar product mark |
| `shell.status-radius` | 9999 | Hub status |
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
| `add-card.radius` | 16 | A skill's card on the Sources list, and its row's focus outline |
| `add-card.check-radius` | 50% | Hit area and focus outline of a skill's round check |
| `source-card.radius` | 18 | Card of the open source on Sources |
| `source-card.tile-radius` | 10 | Its owner tile |
| `hunk.radius` | 14 | Hunk block |
| `hunk.pane-radius` | 10 | Yours / theirs panes (outer corners only) |
| `diff.segment-radius` | 4 | Progress segment |
| `popover.radius` | 16 |  |
| `dialog.radius` | 24 |  |
| `sheet.radius` | 20 |  |
| `log.output-radius` | 10 | The activity log's block of lines |
| `palette.radius` | 20 |  |
| `palette.row-radius` | 12 | Palette rows |
| `key-hint.radius` | 9999 | Key hint |
| `toast.radius` | 12 | Sonner's `--border-radius` |
| `floating-bar.radius` | 9999 |  |
| `check.mark-radius` | 50% | Startup status circle |
| `check.hint-radius` | 8 | Startup check hint callout (`tone.danger` for a failure, `tone.attention` for a warning: bg + 1px border) |
| `startup.logo-radius` | 12 | Product mark on the startup layer |
| `tooltip.radius` | 8 | Tooltip |
| `markdown.code-radius` | 4 | A code span in inline Markdown |

## 6. Components

Each entry gives a component's structure, tokens and states, and the reason for a choice that is not obvious. What a component says, and which command a control runs, is in its code.

A control that runs a command is pending from the command until the inventory that follows it arrives: `aria-busy`, a `Spinner` in or beside it, and no input. It keeps the state the last inventory reported, so the screen never shows the outcome ahead of the CLI.

Finder in this file means the platform's file manager. `src/lib/reveal.ts` names it for the platform `src/lib/platform.ts` reads; a screen never checks the platform itself.

### Values

The value of a label/value pair (a `<dd>`: the skill's Details, a source's facts, Settings › Storage) is selectable, so it can be copied; its label is not. A value that is a link (`src/components/selectable-link.tsx`) opens on a click, while a drag across it selects its text and opens nothing.

### Buttons (height / padding / colours)

- **Primary** – `button.primary-height` / 0 `button.primary-padding-x` / `button.primary-bg`, `button.primary-text`, 600; hover `button.primary-hover`. Disabled: `opacity:.3` and inert, never a grey fill.
- **Secondary** – `button.secondary-height` / 0 `button.secondary-padding-x` / `button.secondary-bg`, `button.secondary-text`, 500; hover `button.secondary-hover`.
- **Ghost** – `button.ghost-height` / 0 `button.ghost-padding-x` / `button.ghost-bg`, `button.ghost-text` (or `text.muted`); hover `button.ghost-hover`, `text.primary`.
- **Small, wrapping** – for a label that may wrap, such as Show all under HISTORY: at least 34 high, 6 `button.ghost-padding-x`, left-aligned; the label wraps in `timeline.line` lines and the button grows with them, never clipped or scrolled.
- **Plain, inline** – no fill, no padding, no radius, as tall as its text; `button.ghost-text`, hover `text.primary`. For a line of text that expands, such as an entry of the History sheet; small (`sm`) with its padding, a toast's action (§6 "Toast action").
- **Accent, inline** – the plain inline shape in `accent.primary`, 500, hover `accent.primary-hover`. Only for the call to action at the end of a list's group header, such as Import all, so it sits apart from the plain inline buttons.
- **Link, inline** – the plain inline shape in `text.secondary`, 500, hover `text.primary` and underlined. Only for a link-like action inside a line of text that opens something, such as See changes on a row of the Update skills dialog.
- **Danger** – ghost in `button.danger-text` on `button.danger-bg`; hover `button.danger-hover`.
- **Icon** – `button.icon-size` square, `button.radius` or `button.icon-radius`, `button.ghost-bg`; hover `button.ghost-hover`.
- **Filter row** – the controls of a list's filter row (§2.2 "Filter row"), `Button` variants: the Filter button (`filter`, `filter.height` / 12 / `filter.bg`, `filter.text`, 500; hover `text.primary`; `filter-active` with `filter.active-bg`, `filter.active-text` while a filter is applied, its count a `Badge`, §6 "Pills (status)"); the sort toggle (`filter-toggle`, 28 / 8) and the expand toggle (`filter-toggle`, a 28 square) in `filter.sort-text` on `button.ghost-bg`, hover `button.ghost-hover`, `text.primary`, disabled at 50% like a pending segmented control; a filter chip's cross (`filter-chip`, a 22 square inside the chip, no fill of its own, hover `filter.chip-hover`, `text.primary`).
- **Toast action** – the app's small plain button (Undo, Retry, Cancel), in place of Sonner's: no fill, `button.ghost-text`, hover `text.primary`. It is never the toast's tint, so it does not read as a CTA. A toast's Dismiss stays Sonner's corner close button: app buttons are for actions.

Every button but the inline ones and a filter chip's cross has a resting fill with the button radius, so it reads as a button on every surface. The fills keep three levels: primary (accent), secondary (solid grey), ghost (the quietest). Ghost and danger fills are light tints rather than solid greys, so they hold on the page, cards, toned banners, dialogs, sheets, popovers and toasts alike.

Two buttons side by side: `button.gap`, primary on the right.

### Run button

A primary button whose command can be stopped, such as Add on the Add skills bar (`src/components/run-button.tsx`). While its command runs it keeps its size: its label stays in place but hidden, under a `sm` `Spinner` in `button.primary-text`, and pointing at it or focusing it swaps the spinner for Stop; a click stops the command. The pointer that clicked it shows Stop only after it leaves and comes back, so a click shows the spinner first. It is always as wide as the wider of its label and Stop, so neither the start nor a hover moves it. While it runs it is `aria-busy` and named by what a click does, such as Stop adding.

### Pills (status)

shadcn's Badge (`src/components/ui/badge.tsx`); a pill that is a button is `BadgeButton`, the Badge on a `<button>`. Height `pill.height`, padding 0 `pill.padding-x`, `pill.radius`, `type.pill`; a plugin's version takes `type.version-pill`. The same Badge draws the round count badges: the Filter button's (`filter-count`: `filter.badge`, `filter.badge-bg`) and the agent picker's "+n" button's (`picker-count`, §6 "Agent picker"), `type.key-hint` `on-accent`.

- Not published: `pill.neutral-*`. The only status pill a skill row carries.
- Disabled (plugin, and a machine's disabled agent configuration): `pill.disabled-*`.
- Version (plugin): `pill.version-*`.
- Fork: `pill.neutral-*`, hover `pill.neutral-hover`, the 12px fork glyph then the fork's name. In the detail header of the original of a fork made under a new name, one per fork; it opens the fork.
- Universal: `pill.universal-*`, after the name of a universal agent in the agent picker (§6 "Agent picker"). It does nothing on a click: it is a button only so that its tooltip, which says what universal agents are, opens from the keyboard too.
- This machine (Machines): `pill.neutral-*`, on this machine's row and beside its label in the detail pane.
- Modified (Machines): `pill.neutral-*`, after the name of a library skill edited on its machine.
- Update (sidebar): `pill.update-*`, hover `pill.update-hover`, 20 high, the `2xs` arrow-up icon then how many updates the app offers. §6 "Sidebar" places it.

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
| Locked | 45% opacity, `cursor:not-allowed`; a click toasts the reason, with the adopt action when adopting the skill unlocks its chips |
| Displaced | on, with a `type.technical-sm` line in `tone.attention.fg` under the name; inert |
| Inherited | on, seen only through another agent's placement, as Cursor reads Claude Code's skills folder; inert |
| Inert | the switch at 50% opacity, `cursor:default`, no hover border; a click runs nothing, and the Tooltip (§6 "Tooltip") says why and how to change it. The chip keeps focus, so the keyboard opens the Tooltip too |

The Universal chip, on, opens the Remove dialog: a universal client reads the library, so a skill leaves it only by leaving the machine.

The chip of an agent whose folder another agent reads names both in its title and its toast: "Remove from Claude Code and Cursor".

- Order: the chips that are on, then the rest by the popularity of the agent client (`src/lib/agent-popularity.ts`), the clients it does not rank by name. Universal stands where its most used client would. The order is fixed when the pane opens, so a chip stays in place when it is toggled.
- A machine can have dozens of agent configurations, so the section shows two rows of chips, measured at the pane's width, and a small ghost button in the second row, centred on the chips, that shows the rest and hides them again. Every chip that is on, or was on since the pane opened, stays in view, in more rows when two cannot hold them. No button when every chip fits in two rows.

### Agent picker (Add skills bar, Add skill dialog)

The install targets in the floating bar of the Add skills screen, and under the skills found in the Add skill dialog, start with the Universal stack when any configuration reads the library: its 28px logos in the chosen ring of a logo toggle (2px `accent.primary`, offset 2px), with the lock mark (§6 "Checkbox (popover)") on its bottom-right corner, ringed in `overlay.bg`. It is no control: universal clients read the library, so every skill reaches them. Its Tooltip says so, a screen reader also hears the clients' names, and a divider separates it from the rest. Then "Add to" in `type.secondary` `text.muted`, then round 28px logo toggles (`Toggle` `kind="logo"`), at most five: the chosen ones first, then the rest, each group in inventory order. The order is fixed when the bar or the skills found appear and again when the agent picker closes, so a logo stays in place while it is toggled. The choice starts as the enabled configurations, where `skill add` installs when no agent is named (`src/screens/sources/agent-targets.tsx`). With more than six configurations that take a placement, a small secondary button "+n" comes last, n being how many it hides; with six or fewer, the bar shows them all. When chosen targets are among the hidden ones, the button ends with an `agent-picker.badge` round badge in `agent-picker.badge-bg`, `type.key-hint` `on-accent`, that counts them (`Badge`, `tone` and `size` `picker-count`). Each logo shows the client's name in a Tooltip. The bar does not count the selected skills: its Add button does. While the install runs, Add is a Run button (§6 "Run button") and the install's progress runs on the bar's rim (§8 "Progress rim"); the bar shows no text for it.

The "+n" button opens the agent picker, a dialog (§6 "Dialog") titled by where the skills go, with no count. Its subtitle asks for the agents and words the skills as one or several; when configurations read the library, it adds that universal agents always have them, and that term is a help term (§6 "Tooltip") whose tooltip says they read the library, the path in `type.technical-sm`. Then a search field (§6 "Inputs") that filters by name, then every configuration that takes a placement and every universal one, sorted by name together, in a list that scrolls past `agent-picker.list-max-height`. A row has a 28px logo and the name in `type.body` `text.primary` over the configuration's path in `type.technical-sm` `text.caption`. A row that takes a placement is the whole hit target, `agent-picker.row-radius`, hover `agent-picker.row-hover`, with the round checkbox (§6 "Checkbox (popover)") at the right; it toggles the same targets as the bar. A universal row is no control and has no hover: the Universal pill (§6 "Pills (status)") after the name, with the same tooltip, and the lock mark where a row has its checkbox, which a screen reader hears as always on. The search filters both kinds alike. The footer has a ghost Clear, which unchecks every target that takes a placement, and a primary Done, which closes the dialog. The search field has the focus when the dialog opens.

### Checkbox (popover)

shadcn's Checkbox (`src/components/ui/checkbox.tsx`). 16×16 round, inset ring 1.5px `checkbox.ring`; checked: `checkbox.checked-bg` fill, `checkbox.check` mark; mixed, for a box that stands for several of which some are checked: the same fill with a `checkbox.check` dash; locked, for a target that is always on: the same fill with a `checkbox.check` lock (`LockMark`). In a dialog the same mark sits beside a `type.body` `text.primary` label, with a `type.secondary` `text.muted` line under it when the choice needs explaining. A row of the Filter popover's AGENTS is the label of its checkbox, so the whole row is the hit target and shows the focus ring.

### Radio group

One choice among a few (`src/components/ui/radio-group.tsx`): a column of rows `gap-2.5` apart, each the round mark of §6 "Checkbox (popover)", checked on the one chosen, then the label in `type.body` `text.primary`, `gap-2.5` between them. The whole row is the hit target, and the arrow keys move the choice.

### Segmented control

shadcn's Toggle Group (`src/components/ui/toggle-group.tsx`, `kind="segmented"`), one of: the segments are radios, Tab reaches the chosen one, the arrow keys move between them and Space or Enter chooses. One segment is always chosen: a click on the chosen one does nothing. Track `segmented.track-bg`, `segmented.track-border`, `segmented.radius`, `segmented.padding`, `segmented.gap`. Segments `segmented.height`, `segmented.radius`, `type.segment`. The selected segment fills with its meaning colour (`segmented.*-selected`) and `on-accent`; the others are transparent in `segmented.text`.

On a card the track is `segmented.card-track-bg`. A control whose command runs is pending like a switch (§6 "Switch"): disabled at 50% opacity, `aria-busy`, with a `Spinner` beside its label, until the inventory that follows arrives.

### Agent client logo chip

Round, `logo.bg`, inner padding by size (`logo.padding*`), the image contained. Sizes: 16 (filter chip), 20 (list rows, popover), 24 (server rows), 28 (toggle chips), 44 (agent client cards). Stacked logos overlap by `logo.overlap` with a `logo.ring` ring in the parent's background. Every logo names its client on hover in the Tooltip (§6 "Tooltip"), which opens at once. A logo whose control already shows the name or carries its own Tooltip (a toggle chip, a logo toggle, the Universal mark) shows none.

- The logos are bundled (`src/assets/logos`) and never fetched. Each is the client's official mark as the vendor publishes it, never one drawn for the app.
- A logo is never dimmed or greyed. A client that is off says so in text and in the title, and a server row shows only the agents that have the server on.

- A stack that hides some clients ends with "+n" in `type.technical-sm` `text.hint`, 4px after the last logo (6px for 24 and 28px logos); after 44px logos, in `type.agent-card-name` `card.text`, 12px after. Pointing at the stack opens an agent list card (`AgentListCard`): a hover card (§6 "Hover card") that lists every client, drawn and hidden, a 16px logo beside each name in `type.secondary` `text.primary`, in two columns that scroll past `agent-list.max-height`. The card is as wide as the list, at most `popover.width`, so the list sits 14px inside every edge: the first logo 14px from the top, the longest name 14px from the right. The list runs to the card's top and bottom edges: its 10px padding (plus the row's own 5px above its logo) sits inside the scroll, at its start and end, so a scrolled row is cut at the card's edge, and a row cut at the bottom says the list scrolls. Its logos then name no client in a Tooltip of their own. It opens below the stack, ending at its right edge; for a stack inside a menu, it opens beside the menu instead, so it covers none of the menu's rows. Like a tooltip, a hover card stacks over a menu.

### Agent filter chip

The chip after the Filter button echoes the agent filter (`FilterTagsChip`). One agent: its logo and name, with a cross that clears it. Several: at most three 16px logos and how many, such as "12 agents", with no cross. Pointing at the chip, or tabbing into it, opens a panel from the chip's own place, over the list and pushing nothing: the search field's surface (`input.bg`, 1px `input.border`) with `popover.radius` and `popover.shadow`, `filter-tags.width` wide. The same logos and count head it, and one tag per chosen agent follows: a 24px pill in `filter.chip-bg`, the agent's 16px logo and name in `type.secondary` `text.secondary`, hover `filter.chip-hover` and `text.primary`. The tags start where the summary's logos do, 8 from the panel's edge. They wrap and scroll past `filter-tags.max-height`, and run to the panel's bottom edge with no padding below them, so a tag cut there says the list scrolls; their bottom padding comes only at the end of the scroll. A tag is a button that takes its agent off the filter; pointing at it or focusing it shows a cross over the end of its name, on a fade of the hover fill. The panel has no close control: it goes when the pointer or the focus leaves it.

### Agent client logo chip, monogram

A client with no bundled logo shows two letters of its name, drawn as an image like every other logo: the initials of its first two words when the name has more than one (words split at spaces, hyphens and underscores), or its first letter and the next for one word. They are semibold, on a round tint that fills the logo's box with no padding: `monogram.N.bg` behind, `monogram.N.text` for the letters, read from the tokens as the page has them, where N comes from a hash of the name, so a client keeps its hue everywhere and on every launch. Ten hues tell more clients apart than one white tile with a letter did, and the dark tint sits with the app's surfaces where a white tile would glare.

### Skill glyph (16px, leading the row)

The dot and fork glyphs say one thing: whether there is an action to take. `glyph.update` (the accent) when the skill has an update to apply, else `glyph.idle`. Whether a skill is enabled is not the glyph's to say: a row in no agent client is washed out (`row.off`) instead.

- dot (filled) – a skill of a shared source.
- pencil – edited locally; `glyph.edited`.
- fork – a hub skill.

Sort toggle, at the right end of the filter row: the list always sorts by name within each group, whatever the state, and the toggle always shows it in `filter.sort-text` on `button.ghost-bg`, so it never reads as unselected. A-Z with the arrow at first; each click flips between A-Z and Z-A (the arrow turned). Disabled skills sort with the rest and read as washed out.

Expand toggle, just before the sort toggle: a 28px icon button in `filter.sort-text` on `button.ghost-bg`, like the sort toggle, with its name in a tooltip. It opens or closes every section of the list at once, groups and plugins' subgroups alike. Collapse all (two chevrons pointing together) while any group is open: it collapses every group to its header, and the detail pane keeps the selected skill. Expand all (two chevrons pointing apart) while all are collapsed: it opens every group and every plugin's subgroup the filters list. Disabled while the filters list no skill.

- fork marker (12px, after the pill and the alert triangle) – the original of a fork made under a new name, in `text.caption`, one per fork. A marker, not a control: the fork is listed once, under hub skills.

### Group header (lists)

Every group's header is a section toggle (`src/components/section-toggle.tsx`, `level="group"`), the trigger of a shadcn Collapsible (`src/components/ui/collapsible.tsx`) that holds the group, like a plugin's below but with `group.padding` and the group's colours: a `xs` chevron in `text.caption`, `type.caption` in `group.label`, then the count in `group.count`. A click collapses or expands the group's rows (`aria-expanded`, with `aria-controls` naming the rows); a collapsed group keeps its header and count, and j/k skip its rows. Every group is open when the app starts, and the list keeps which are collapsed while another screen shows. A skill selected in a collapsed group, such as one another screen opens, opens its group. A source's group shows only the source's short form, so its label shows the source's full URL in a tooltip (§6 "Tooltip"), above the label and aligned to its start. `group.padding`, with `group.spacing` above each group but the first, and at least `group.header-height` tall. A header can end with one control at its right edge: the Unmanaged group's Import all, an accent inline button (§6 "Buttons") in `type.button-sm`, as tall as its text, while another tool installed skills agentx can adopt. It opens the adopt dialog (§6 "Adopt dialog") for every such tool. No header holds a control to open or close every section: the expand toggle in the filter row does that for the whole list. A header can carry one mark after its count, a pill that says a state of the whole group, such as Disabled on a disabled agent configuration of a machine.

Nesting: what a group holds is inset by `subgroup.indent`, a few pixels clear of the guide line, so a row's fill never touches it, and a 1px `list.guide` line runs down the left of it, under the group's chevron (`list.guide-inset`). A plugin's rows sit one level further in (`subgroup.nested`), with a second line under the plugin's chevron (`list.guide-nested-inset`). Each placed item draws its own piece of the line (`src/components/list-guide.ts`), so the line runs on through the rows and the spaces between them. Any other grouped list with section toggles uses the same pattern: the tokens and `LIST_GUIDE` / `LIST_GUIDE_NESTED`.

The PLUGINS group holds one subgroup per plugin, A to Z by the plugin's name; a plugin installed in several agent clients is one subgroup. Its header (`src/components/section-toggle.tsx`) is a flat, full-width row, not a button with a fill: `subgroup.padding`, `row.radius`, no fill at rest and `subgroup.hover` under the pointer; a `xs` chevron in `text.caption`, the plugin's name in `type.caption` `subgroup.label`, then the count in `type.technical-sm` `subgroup.count`, `group.gap` apart. The plugin's marketplace sits at the header's right end in `type.technical-sm` `subgroup.source`, in the space the name and count leave: it truncates first when the column is narrow and disappears when no space is left, and its tooltip shows it in full. The plugin's name truncates only once the marketplace is gone. It tells apart two plugins of one name from two marketplaces, which are two subgroups. The version is left out: one subgroup can hold several agent clients' installs, each at its own version. It sits `subgroup.indent` in from its group's header, so its chevron sits under the group's caption. `subgroup.spacing` above each header but the first in its group, outside its pinned header. The plugin's rows are inset by `subgroup.nested`, so each row's glyph sits under the plugin's name and the rows read as inside its section; their hover and selected fill start at the inset. A click collapses or expands the plugin's rows (`aria-expanded`); the chevron turns 90° while they show, with no transition. A collapsed subgroup keeps its header and count, and j/k skip its rows. Every subgroup is collapsed when the app starts, and the list keeps which are open while another screen shows. The subgroup of a skill that is selected while collapsed, such as one another screen opens, opens so the list shows the skill. Collapsing it again keeps it collapsed. Each row's place (`aria-posinset`, `aria-setsize`) is among its plugin's rows.

The section toggle also heads a source in the Update skills dialog (§6 "Update skills dialog"), with no count. There it can end with one control at its right end, `subgroup.padding` from the edge, beside the toggle and outside it, so a click on the control toggles nothing; the whole row then takes `subgroup.hover`.

### Alert

shadcn's Alert (`src/components/ui/alert.tsx`): `Alert` with its `tone` (`attention`, `danger`, `neutral`, `primary`, `yours`), `AlertTile`, `AlertContent` holding `AlertTitle` and `AlertDescription`, and `AlertAction`. In its `banner` layout, a box: `banner.radius`, `banner.padding`, `tone.*.bg` with a 1px `tone.*.border`; a `banner.tile` icon tile in `tone.*.fg` (the `RefreshIcon` for an update, the `AlertIcon` otherwise); title `type.banner-title` `banner.title`, text `type.secondary` `banner.text`; its buttons on the right, vertically centred, wrapping to a line of their own under the text, still on the right, when they would leave the text narrower than `banner.text-width` (240px). That wrap is why the box is a wrapping flex row and not shadcn's grid. The detail pane's banner and the notice under AGENTS when an agent sees more than one skill of a name are alert banners; nothing else draws a box like it.

A banner's role is `note`, named by its title, not shadcn's `alert`: a screen reader would announce an alert each time a skill opens. In its `bar` layout, the stale bar (§6 "Stale bar"), it is an `<output>`, a polite status region.

### Banner (detail pane)

An alert banner (§6 "Alert").

- The detail pane shows one banner, the first that applies in the ordered table in `src/screens/skills/banners.ts`. The header shows no status pills: the banner is the status.
- A banner that says where the skill came from sets `coversOrigin`, and the header then leaves out the origin text beside the name. Today only the banner of a skill another tool added does.
- The origin text beside the name of a source's skill, or of a fork of one, is a link when its source has an https page, the page Source or Forked from in Details opens: `text.caption`, `text.secondary` and underlined on hover, the URL as its tooltip.

| Banner, by what it reports                                   | Tone        |
| ------------------------------------------------------------ | ----------- |
| An update that conflicts with local edits                    | `attention` |
| An update                                                    | `primary`   |
| A client that holds its own copy instead of the library's    | `attention` |
| A skill another tool added                                   | `neutral`   |
| An unmanaged skill no tool recorded, which the CLI can adopt | `neutral`   |
| A source that is gone, or a skill gone from its source       | `danger`    |
| A skill in no agent                                          | `neutral`   |
| A hub skill with unpublished edits, or never published       | `yours`     |
| A skill of a source edited locally                           | `attention` |

### Banner actions and dialogs (detail pane)

- A banner's actions are small buttons on its right, one command or one dialog each: ghost by default, primary for the action that applies the update, danger ghost for one that discards work. A destructive action opens a confirm dialog first.
- Adopt and Update are banner buttons only: a skill row carries no call-to-action pill. Every unmanaged skill the CLI can adopt has the adopt action, whether another tool recorded it or not. A skill whose record the CLI refuses, a skill of a project and a symlink in a client's skills directory have none, and the chips' toast then offers no action. The CLI's reason for refusing a record is in the adopt dialog alone (§6 "Adopt dialog").
- The banner of a skill of a source edited locally only informs, with a publish action when this machine can write to the source. It has no fork action: Fork is in the action row.
- The banner of a skill gone from its source only informs. It has no fork action either: Fork is in the action row.
- A command refused because the target folder holds another version opens a confirm dialog, with the path in `type.technical` `text.caption` and a danger confirm.
- The action row sits on the title row, pushed to its right end (`margin-left:auto`), centred on the row and wrapping when it is full, with `button.gap` 6 between its buttons. Remove, last, is a danger ghost button; its dialog lists the agents the skill leaves in `type.body` `text.secondary`, with a danger confirm. Past `dialog.max-height`, only that list scrolls; the title, the consequence and the buttons stay.

### Dialog

`dialog.width`, `dialog.padding`, `dialog.radius`, `overlay.bg` with 1px `overlay.border` and `overlay.shadow`, over `shell.scrim-strong`; blocks `dialog.gap` apart, at most `dialog.max-height` high. Title `type.dialog-title` `dialog.title`, subtitle `type.secondary` `dialog.subtitle`. The footer ends with a ghost cancel and the confirm button (primary, or danger for what cannot be undone); Enter confirms. While its command runs, the confirm shows a `Spinner` and the dialog stays open. A dialog that says how its run stands does so at the footer's left side, in `DialogStatus` (`src/components/ui/dialog.tsx`): `type.secondary` `text.hint`, the same in every dialog. A confirm before a command that deletes or replaces work (Remove, Replace, Detach, Discard my edits) is an alert dialog (`src/components/ui/alert-dialog.tsx`): it looks and moves the same, but a click outside does not close it, Cancel takes the focus on open, and assistive technology announces it as an alert. A field label is `type.secondary`, `input.label-gap` above the field; a hint under it is `type.secondary` `text.hint`. A refusal of what a field holds shows under the field (§6 "Refusal under a field"), and a change to the form clears it.

### Fork dialog (detail pane)

Fork is a secondary small button in the action row, before Remove, for a skill of a source and a plugin's skill. No banner carries it. An unmanaged skill is adopted from its banner. The title is a question. The dialog holds a switch row (§6 "Switch") to fork under a new name; with it on, a mono Name field appears and takes the focus, its naming rule under it in `text.hint` while the name breaks it, and the confirm is disabled while the field holds no valid name. For a plugin's skill the switch starts on, with `my-<name>` in the field, selected, so Fork works at once: a fork under the plugin's own name leaves two skills of that name for the agents with the plugin. Turned off, one `type.secondary` `text.muted` line under the switch says so. A fork that succeeds closes the dialog and selects the fork; no dialog follows it.

### Rename dialog (detail pane)

Rename is a secondary small button in the action row, before Remove, for a hub skill. The title is a question. A mono Name field holds the skill's name, selected, so typing replaces it; its naming rule shows under it in `text.hint` while the name breaks it, and the confirm is disabled until the field holds another valid name. A rename that succeeds closes the dialog and selects the skill under its new name.

### What each agent sees (detail pane AGENTS)

Where the agents find a skill of the selected skill's name, from the inventory alone: which places hold one, and which agents see each. It never says which of two skills an agent uses, since that is the agent's own rule.

- When some agent sees more than one skill of the name, an alert banner (§6 "Alert") under the chips in `attention`: its title says how many skills of the name there are, and its line that agents seeing more than one may pick the wrong skill, ending with Details: the line's type and colour with a dashed underline, plain text that opens a hover card (§6 "Hover card") on hover. The banner has no button, and the hover card only explains: Rename is in the action row. With no agent seeing more than one, AGENTS shows nothing of this.
- The hover card: its title in `type.secondary` `text.primary` 500, then a table (`src/components/ui/table.tsx`). A column for each place, the library first, then each plugin, then each agent's own folder, its caption in `table.header-text`; a check in `tone.primary.fg` where the row's agents see the skill, an en dash in `text.hint` where they do not. The agents that see more than one skill come first, a row each on `tone.attention.bg`; then the agents that see one, a row for those that see the same one, their names joined. Rows are `type.secondary` `text.secondary`, a 1px `shell.divider` rule above each. While some row is highlighted, a `text.muted` line under the table says what the highlight means.

### Edit (detail pane)

Edit is a secondary small button, first in the action row, for a skill the user may write to (`canEdit` in `src/screens/skills/edit.ts`). An unmodified skill of a source has none, because its first edit would quietly turn it into one an update has to merge with. Edit opens the skill's `SKILL.md` in the editor chosen in Settings, runs no command and shows no toast; the inventory that follows shows what changed. When the editor cannot be opened, a toast says so, with Show in Finder as its action.

### Publish dialog (detail pane)

Under the subtitle, an optional message field, then FILES (`type.caption` `text.caption`): one row per changed file, the path in `type.technical` `text.secondary` and its status in `type.technical-sm` `file-row.edited-label`. While the files are read, or when there are none or they cannot be read, one line in `type.secondary` `text.hint` takes their place. While the publish runs the field is disabled and the footer's left side shows its progress in `type.secondary` `text.hint`; the dialog closes when the command ends and its outcome is a toast.

### Adopt dialog

A bulk adopt, Import in the app's words, confirms in a dialog titled "Import skills from <tool>" that lists the skills by name, grouped by tool under a `type.caption` caption when it covers two (`src/screens/skills/adopt-dialog.tsx`). The list scrolls past `adopt.list-max-height`, and gives up room when the window is short, so the dialog stays inside the window. The records the preview refuses are folded under the list, behind one plain inline button with a `xs` chevron, "N skills can't be imported", the trigger of a Collapsible (`src/components/ui/collapsible.tsx`); opened, each shows the CLI's reason under its name, in a list that scrolls past the same height.

The run stays in the dialog. Each row shows its state: nothing while waiting, a `Spinner` once a step of the run names it, a check in `text.hint` once it is installed; then its result, a check in `tone.primary.fg` or a cross in `tone.danger.fg` with the reason under the name in `type.notice` `tone.danger.fg`. The footer's left side (`DialogStatus`) counts the skills started, from the run's `blobs` steps, then sums the result. The dialog cannot be dismissed while it runs, and one primary button closes it afterwards. It shows the result, so no toast does; the activity list still logs each run. A run that fails as a whole is a refusal paragraph (§6 "Refusal under a field") above the footer. A warning the run logs is a `type.notice` `tone.attention.fg` paragraph above the footer, except one about a skill the run did not adopt, whose row says why.

### Update skills dialog

`src/screens/skills/update-dialog.tsx`, which the sidebar's update pill opens (§6 "Sidebar"), and so does Check updates (§6 "Skills header", §6 "Command palette") when the check ends with an update offered: its progress toast then goes, so no toast and the dialog show together. It lists every skill the last update check found an update for, all selected; a skill with a pending merge is left out, since its banner finishes the merge first. Its structure follows the Adopt dialog: title, subtitle, the select-all row of Sources (§6 "Sources screen") when the list holds two skills or more, the list, the footer.

- The list scrolls past `update-skills.list-max-height`. It groups the skills by where the update comes from, the upstream for a fork, else the skill's source, one group per URL even when two share a short form, A to Z by the source's short form, the rows of each A to Z by name. Each group is a section that collapses: its header is the section toggle (§6 "Group header (lists)", `src/components/section-toggle.tsx`), a `xs` chevron and the short form in `type.caption` `subgroup.label`, the full URL as its tooltip, no count. A click on it collapses or expands the group's rows (`aria-expanded`); every group is open when the dialog opens, and Update opens them all again. The header has no Select all or Deselect all: the select-all row above the list selects every skill, and the rows one by one. The rows sit `subgroup.indent` in from the header, as a plugin's rows do under theirs, so they read as inside their source's section; their hover starts at the inset. Groups sit 12 apart.
- A row is the whole hit target, `update-skills.row-radius`, hover `update-skills.row-hover`. Its first line opens with a 16 slot, centred on the name and `gap-2` before it (`button.icon-gap`, as an icon before a label): before the run it holds the round checkbox (§6 "Checkbox (popover)"), and in the run the row's state takes its place, so no name or note moves when the run starts. Then the name in `type.body` `text.primary`, then, `·` apart in `type.secondary` `text.caption`, when the newer version was published in `type.secondary` `text.hint` (relative within a month, its full date and time as the tooltip; left out when the CLI dates it not) and See changes, a link inline button (§6 "Buttons"). A row is two lines at most, so it keeps its height through the run: under the first, one `type.notice` line at most, starting under the name, truncated with an ellipsis and its full text as the native tooltip. Before and during the run, a skill with local edits says there that the update merges with the edits, in `tone.attention.fg`.
- The name is a label of the checkbox whose hit area covers the row; See changes and the checkbox sit above it, so a click on See changes toggles nothing. It opens the What's new sheet (§6 "Sheet (What's new)") of that update, on the dialog's layer and above it: Escape closes the sheet first, then the dialog. The sheet has no trigger, so on close it puts the focus back on that See changes, or on the confirm when the row is gone.
- The footer's left side says when the last check ran, in `type.secondary` `text.hint`. The confirm counts the selection: "Update all N" when every one of two or more is selected, else the number of skills, and plain and disabled at none. Enter confirms; on open the confirm has the focus.
- The run stays in the dialog, as the Adopt dialog's does: one `skill update <name>...` for every selected skill, which the CLI updates in one run. The rows left out leave the list, and a group with none selected goes with them. In each selected row the state replaces the checkbox in its slot before the name, and the slot stays while the state shows nothing, so the names keep their place and one column. The state is nothing while the row waits, a `Spinner` in `text.muted` on every selected row from the start of the run until the run is done with it, then a check in `tone.primary.fg` when it is updated, merged with its edits or was up to date already with no warning, an `AlertIcon` in `tone.attention.fg` when the merge conflicts with its edits or the CLI logged a warning for it, or a cross in `tone.danger.fg`. A warning is not a clean success, so it takes the conflict's mark; the footer still counts the skill as updated, since it was. A screen reader hears the state before the name, "with a warning" added for one. Once the run finished with it, the row's note says what the user must act on, the first that applies: the conflict and where to resolve it in `tone.attention.fg`, or the CLI's reason in `tone.danger.fg`; then the first warning the CLI logged for it, such as a placement it skipped, in `tone.attention.fg`, every warning in the tooltip; else the note on local edits stays. A conflict replaces that note rather than adding a line. Once the run ended, the list keeps only the rows with something to say, a conflict, an error or a warning, under their groups, and a group with none of them goes; a skill updated with no warning leaves the list. When no row has anything to say, the list goes and the footer's sum says it all. A run with a skill not updated, or a warning, makes its activity entry ask for attention. A conflict opens no skill. On close, the focus goes back to the pill, or to Skills once no update is left and the pill is gone.
- The footer's left side counts the run, the skills done plus the one under way ("Updating 2 of 4…"), then says how many of the selected skills it updated. The dialog cannot be dismissed while it runs, and one primary Done closes it afterwards. It shows the result, so no toast does; the activity list logs one entry for the run. A run that ended with no word from the CLI stops, the skills it did not reach waiting again, with its problem as a refusal paragraph (§6 "Refusal under a field") above the footer. The lock, the store and a cancel are the runner's to say, and put the dialog back as it was before the run.

### Create skill dialog

`src/screens/skills/create-dialog.tsx`. Two fields, each with its label in `type.secondary` `text.secondary` and its rule as a hint under it: Name, mono and focused on open, and Description. A hint whose rule the field breaks turns `tone.danger.fg`, the field is `aria-invalid` and the confirm is disabled. A name the CLI refuses shows the refusal under Name in place of the rule; any other refusal shows under Description. The dialog stays pending until the inventory that follows lists the new skill; it then selects the skill and opens its `SKILL.md` as Edit does (§6 "Edit (detail pane)").

### Add skill dialog

`src/screens/skills/add-skill-dialog.tsx`, which Add in the Skills header and the palette open. It works in two steps in one dialog, so the link and what it found stay in view together. The first step only reads: `source skills` lists the skills under the link, from the network for a source not added, and adds nothing. The second, Install, adds the source and installs the skills checked in one `skill add`. Closing the dialog between the two, by Cancel, Escape or a click outside, runs nothing, so a source is added only when a skill from it is installed.

- One mono field for a link to a repository, a folder of skills or one skill, named by its placeholder and focused on open, with a secondary Find skills at its right, `gap-2` apart; a hint under them in `type.secondary` `text.hint`. The button keeps its place by the field because it acts on the field alone, and Find skills says what it does, a lookup, where Continue would say nothing of it and an Add verb would claim a change the lookup does not make. Enter in the field also runs it; it is disabled while the field is empty, and pending (§6) while `source skills` runs, Install being disabled meanwhile.
- What it found shows under the field: the select-all row of Sources (§6 "Sources screen") when there are several, then the skills in a list that scrolls past `add-skill.list-max-height`. A row is the whole hit target, `add-skill.row-radius`, hover `add-skill.row-hover`: the round checkbox (§6 "Checkbox (popover)") in a 16 slot, centred on the name and `gap-2` before it, as in the Update skills dialog, then the name in `type.body` `text.primary`, and under the name, starting where it starts, the description in `type.secondary` `text.muted`, two lines at most. The select-all row is inset as the rows are, so its checkbox heads their column. A skill this machine holds already is not selectable: its row has no hover, its slot stays empty so the names keep one column, and it shows its card status (`add-card.status-*`, the words of the Add skills cards) at the right, the selectable rows first. One skill found is checked; of several, none is. A skill is matched to the library by its source's canonical URL and its subpath, since a source not added has no id.
- When none of the skills found can be installed, a line in `type.secondary` `text.hint` says so in place of the select-all row, above the rows and their statuses, and neither "Add to" nor its targets show.
- Under the list, "Add to" and the targets of §6 "Agent picker", the same control as the Add skills bar, so the two places choose agents alike.
- The footer's primary Install runs one `skill add` for the skills checked, disabled while none is or a lookup runs. The dialog stays pending until the inventory that follows lists them; it then closes and selects the first, and the toast names what was installed and its source. `skill add` changes the machine, so the sources are read again after it and Sources lists the source it added.
- A change to the field clears what was found and any refusal, so Find skills looks the new link up. A refusal of either step shows under the field (§6 "Refusal under a field"), a source git cannot reach as the Add source dialog words it.

### Refusal under a field

A CLI refusal of what a field holds shows under the field as one paragraph, the message then the hint, in `type.notice` `tone.danger.fg`, as an alert (`src/components/problem-text.tsx`). `src/lib/problem-text.ts` sentence-cases each part and ends it with a full stop, and changes nothing else: commands, paths and the skill names the field knows keep the CLI's spelling. When the CLI's hint names a flag or a command, the field shows a hint in the app's own words that names its own control (`src/lib/app-hint.ts`). The error toast says a refusal the same way.

### Agent client card (Agents page)

One card per agent configuration whose client does not read the library, in a 4-column grid with `agent-card.gap`: `card.bg`, 1px `card.border`, `agent-card.radius`, `agent-card.padding`.

- Top row: the `agent-card.logo` logo on the left, the Enabled switch on the right.
- Then the name in `type.agent-card-name` `card.title`; the configuration path in `type.technical` `text.caption`, one line with an ellipsis; the counts in `type.secondary` `card.text`, numbers in `text.primary` 500.
- Enabled decides only where later installs and creations go when no agent is named. It never changes a placement, so the counts do not move when the switch does.
- Last, at the card's foot so it lines up across a row: the placement, a `type.caption` `text.caption` caption over a full-width segmented control (§6 "Segmented control", on a card) with Symlink and Copy, the selected segment `segmented.neutral-selected`. A segmented control, not a switch or a menu: two named choices that both show at once, the same control as the filter's State, and it fits the narrowest card at the minimum window. Its `title` explains both choices.
- The placement decides only how later placements in the configuration are made (an add, a place, Place all, a create, a fork): skills already placed keep their kind, so the counts do not move either. When the configuration holds skills of the other kind, the toast that confirms the switch offers Convert existing, which converts them (`--convert`); its own toast counts what it converted and the edited copies it kept. A toast action and not a dialog: the switch changes no file, so it needs no confirmation, and converting is the choice the user makes after it. The control shows the inventory's `placement` and is pending until the inventory that follows the command arrives; it is also disabled while the switch's command runs.

### Removing an agent configuration (Agents page)

- Every card, and every column of the Universal card, has a ghost icon button with the `MoreIcon`, after the switch on a card and at the right of the name in a column, that opens a menu (§6 "Menu") with Remove.
- Remove opens a confirm dialog: the directory that is deleted in `type.technical` `text.caption`, a ghost cancel and a danger confirm. The app asks nothing else: the user decides what goes, and the CLI deletes the directory whatever it holds.
- The dialog closes at once and the menu button is disabled until the inventory that follows has arrived; the card goes with that inventory.

### Universal card (Agents page)

One full-width card above the grid, the same surface, for every configuration whose client reads the library. A lead column `universal-card.lead-width` wide: the clients' logos stacked (`agent-card.logo`, ringed in `card.chip-ring`), at most five then "+n", whose hover lists every client (the agent list card), the title in `type.agent-card-name`, a line in `type.secondary` `card.text` and the library path in `type.technical` `text.caption`. Then one column per client behind a 1px `shell.divider` on its left: name, path and counts, the skill count being the library's. The columns share the rest of the width and wrap into more rows when a row would make them narrower than `universal-card.column-min-width`. No switch and no placement: a universal client sees every library skill whatever its setting, through the library itself.

### Tabs

shadcn's Tabs (`src/components/ui/tabs.tsx`) with its default look on our tokens: a `tabs.list-bg` track, `tabs.list-radius`, `tabs.list-height`, `tabs.list-padding`, as wide as its tabs; each tab `type.tab` in `tabs.text`, `tabs.trigger-radius`; the active one on `tabs.active-bg` with a 1px `tabs.active-border` edge and `tabs.active-text`. The active tab's content follows `tabs.gap` below. A tab can end with a count in `type.technical-sm` `text.caption`. With `fill` on `Tabs` and on each `TabsContent`, the tabs take the rest of the column's height and pass it to the content; what scrolls is the content's own child, such as a `Table` with `scroll`. A tall panel with no such child is clipped, not scrolled.

### Plugins screen

The Plugins screen (`src/screens/plugins`) is a split, as Skills is: the list column on the left, the open plugin on the right. It is read-only: nothing on it installs, removes, enables or disables a plugin.

- The list column: the serif title and the plugin count, the search field (§6 "Inputs"), then the filter row: the Filter button, whose popover holds AGENTS, the agent configurations that hold a plugin, each its own choice with no Universal (§6 "Popover (Filter)"), the agents chip while any is checked, the expand toggle and the sort toggle (§6 "Skills list"). The expand toggle reads Collapse all while any group is open and collapses every group to its header, with the open plugin kept in the detail pane; it reads Expand all while all are collapsed and opens every group. It is disabled while the filters list no plugin. The sort toggle is A-Z by name at first, and each click flips between A-Z and Z-A. Disabled plugins sort with the rest and read as washed out.
- The plugins, one row per plugin in one agent configuration in one version, are grouped by agent configuration, A to Z by its name. A group header is a section toggle (§6 "Group header (lists)", `level="group"`): the chevron, the configuration's 16px logo, its name and count. It pins to the top while its group shows, and a click collapses or expands the group. Every group is open at first; a plugin opened from Also installed in opens its group. The rows follow the nesting pattern there: inset by `subgroup.indent`, with the `list.guide` line under the chevron (`LIST_GUIDE`). A row (`src/components/list-row.tsx`) holds the `plugin.row-tile` tile, the name, the Disabled pill when the client turned the plugin off, and the version at the right end in `type.technical-sm` `text.caption`; under them, how many skills, servers and hooks it brings, in `type.row-desc`. A disabled plugin's name and counts are at `row.off`; its tile is not.
- The detail pane: the `plugin.tile` tile, the name in `type.plugin-name`, the version pill and the Disabled pill. Under them, the detail facts (§6 "Detail facts"): the agent, always with its logo; the marketplace; the path, after a `xs` folder icon in `text.caption`, a link that shows the plugin's directory in Finder; and, when other agents hold the same plugin from the same marketplace, each of them with its logo and version, a link that opens it.
- Then the tabs (§6 "Tabs"): Skills, MCP servers and Hooks, each with its count, and only those the plugin has; a plugin with none has no tabs. Each tab is a table (`src/components/ui/table.tsx`): a skill and its description, a server and its transport, a hook and its event. A skill's name opens it in Skills, a server's in MCP servers; a hook opens nothing. The name column is `plugin.name-col` wide and the other column takes the rest. A name wraps to at most 2 lines: a longer one ends in an ellipsis, and pointing at it shows the whole name in a tooltip. A description renders its inline Markdown (§6 "Inline Markdown"), shows at most 2 lines and ends in an ellipsis past them, with no tooltip.
- Only the table scrolls. The title, the details and the tabs stay in place, and the table's column captions pin to its top on `shell.content-bg`, so rows pass under them.
- The tile is the plugin's own icon from the inventory, or the name's first letter in `type.plugin-tile` on `button.secondary-bg`.

### Detail facts

The facts under a detail pane's title (`src/components/detail-facts.tsx`), as under a plugin's name and a machine's label: one row that wraps, `detail-facts.gap` apart, each a `type.secondary` `text.hint` label over its value in `type.technical-sm` `text.secondary`.

### Machines screen

The Machines screen (`src/screens/machines`) is a split, as Plugins is: the machines on the left, the open machine on the right. This machine is the applied inventory, and every other machine is what the hub holds of it. Nothing on it compares two machines.

- The list column: the serif title and the machine count, with a `Spinner` in `text.hint` beside the count while the hub is read. It has no search, filter or sort: the list is flat and short, and its rows start `split.list-top` under the header. This machine comes first, then the other machines A to Z by label, then the cloud machine, which goes by its glossary name.
- A row (`src/components/list-row.tsx`) holds the label, the This machine pill on this machine's row and, at the right end, the 20px logos of the machine's agents, at most 4 with a "+n" count for the rest (§6 "Agent client logo chip"). Under them, in `type.row-desc`: when the machine's report last changed, as time ago within 30 days and as a date after that, then how many skills its agents see: the names in the machine's details, each counted once, so that a row and its details agree. This machine's row gives no time, since it shows the machine as it is now.
- While no other machine can show, one centred `type.secondary` `text.hint` line stands 24 under the rows, with at most one ghost small button under it.
- The detail pane: the label in `type.machine-name`, then the This machine pill at `detail` size on this machine. The cloud machine has one `type.detail-desc` `text.muted` line that says what it is, `detail.header-gap` under its name. No machine is removed from here: a machine leaves the list when it unlinks the hub. Then the detail facts (§6 "Detail facts"): the machine id, the operating system and the agentx version a report names, and when the report last changed as a date and time. This machine shows its id alone, and the cloud machine, which has no id of its own, when it last changed.
- Then the skills each agent of the machine sees, one group per agent configuration, A to Z by its name, as the Plugins list groups them: a section toggle (§6 "Group header (lists)", `level="group"`) with the chevron, the configuration's 16px logo, its name and count, and the Disabled pill as its mark on a disabled configuration. A header pins to the top while its group shows, and every group is open at first. The groups reach 14 past the pane's padding on both sides, so a chevron sits under the label's left edge, as a list column's chevrons sit under its title.
- A skill's row follows the nesting pattern there: inset by `subgroup.indent`, with the `list.guide` line. It is 36 high with 8 12 8 14 padding, has no hover and opens nothing. Its left half holds the name in `type.row-name` and the Modified pill; its right half starts at the row's middle, so it reads as a column across the groups: what brings the skill in `type.secondary` `text.muted`, then, for a library skill, where it is published in `type.technical-sm` `text.caption`, a source in its short form with the full URL as its tooltip. In a group, the library's skills come first, then a plugin's, then those of the agent's own skills directory, each A to Z.
- Only the groups scroll. The label and the facts stay in place.

### Copy button

`src/components/copy-button.tsx`: a ghost button with the copy icon before its label, such as Copy in the activity log and Copy report on the startup layer. Once the text is on the clipboard, the icon is a check in `tone.primary.fg` for 2s and a screen reader hears Copied; no toast shows, as the button answers itself. A copy that fails is an error toast.

### Toast

A toast reports what happened; it asks for nothing. A control the user is expected to use, such as the Add skills bar (§6 "Agent picker"), is a floating bar at the bottom instead, so the two never cover each other.

shadcn's Sonner toast (`src/components/ui/sonner.tsx`), with its own size, padding, type and shadow, its corner close button, the app's button for its action (§6 "Buttons", "Toast action") and the app's colours: Sonner's colour variables are set from the toast tokens in `src/styles/index.css`, where shadcn sets them with `style`. `toast.offset` below the title bar and from the right edge of the window, so a toast never covers the title bar or its sync status (§6 "Publish status (title bar)"); `overlay.bg`, `overlay.border`, `toast.radius`; text `toast.text`.

Its kind (`ToastKind` in `src/components/toast.tsx`) sets its icon and its tint, Sonner's rich colours: the fill, the border, the message and the icon take the kind's toast tokens.

| Kind | Icon | Colours | For |
| --- | --- | --- | --- |
| Success | `CircleCheckIcon` | `toast.success-*` | A command that succeeded, such as "Added 2 skills" |
| Error | `CircleXIcon` | `toast.error-*` | A command that failed, or something the app tried that failed, such as "Couldn't open …" |
| Warning | `AlertTriangleIcon` | `toast.warning-*` | A command that succeeded with warnings, or one that needs the user first, such as "Another agentx command is running" with Retry |
| Progress | `Spinner` | plain | A command that runs, with its Cancel |
| Plain | none | plain | A report with no outcome, such as Stopped, or an action that did nothing |

- Lines under the message, such as an error's details, are Sonner's description in `toast.description`; a warning among them in `tone.attention.fg`.
- An action, such as Undo or a progress toast's Cancel, is a small plain button (§6 "Buttons", "Toast action") at the end of the row; clicking it also takes the toast away.
- One toast at a time: a new one replaces the one showing.
- It goes after 2.6s, or 5s with an action.
- A toast for a command that runs, with its cancel, stays until the command ends and the toast of its outcome replaces it. Progress changes its text in place, the count in `tabular-nums`: Check updates says "Checking for updates…", then "Checking for updates… 1/10".

### Attention toast

A command that exited 0 and logged warnings: a warning toast with the success text, each warning the CLI logged under it, its action when it has one, and Sonner's close button, named Dismiss. It goes after 5s, and its activity entry is `attention`, not `success`.

### Error toast

A command that fails for a reason the app does not handle itself (`src/store/runner.ts`): an error toast with the CLI's message and hint as one paragraph (§6 "Refusal under a field"), the lines that say more under it, as the CLI wrote them, then each warning; its action when it has one, and Sonner's close button, named Dismiss. It stays until dismissed when it has a hint, and goes after 5s without one.

### Stale bar

A bar across the top of the main panel while the screens show an inventory that may be out of date (`src/app/stale-bar.tsx`), the Alert's `bar` layout (§6 "Alert"), an `<output>`: `stale-bar.height`, `stale-bar.padding`, a 1px bottom border, text `type.notice` `text.secondary`. While serve restarts: `tone.attention` with a `Spinner`. When a scan fails or serve stops for good: `tone.danger`, the reason on one line with an ellipsis, and a small ghost retry on the right. Nothing shows while the inventory is fresh.

### Machine label

Under the product name in the sidebar: `type.machine-label`, `text.muted`, one line with an ellipsis.

### Timeline (history)

A list of entries, no dots or connector. Subject `type.timeline-entry` `text.primary`, meta `type.timeline-meta` `timeline.meta`. In the subject of the commit that created a fork, the origin's name (the plugin, the source with its path, or the skill) is a link when the CLI knows its https page: same type and colour, always underlined, the URL as its tooltip; it opens the page in the browser. For a fork of a source's skill, the fork's name after "Fork" is a link the same way, to the page of the skill's directory at the forked commit, when the CLI knows it. The rest of the subject is text. When the list is cut short, the last item is a ghost button that opens the History sheet, its label aligned with the subjects.

### Menu

A list of actions under a button (`src/components/ui/dropdown-menu.tsx`): `menu.min-width`, `menu.radius`, `overlay.bg` with 1px `overlay.border` and `menu.shadow`, `menu.padding`, `menu.offset` below its trigger and aligned to its end, `popover.window-inset` from the window's edges. It flips above its trigger when there is more room there, is at most `menu.max-height` high and scrolls past it, and is at most `menu.max-width` wide. A row is `menu.row-radius`, `menu.row-hover` when pointed at or focused, with a `type.body` `text.primary` line over a `type.technical-sm` `text.caption` line such as a path. In Show in Finder's menu, a place's first line starts with its agent's 20px logo; symlinks of several agents that lead to one place are one row, its line the agents' logos stacked (ringed in `overlay.chip-ring`), at most four, then "+n" for the rest, which opens an agent list card (`AgentListCard`) to the menu's left. A symlink's caption is a neutral pill (§6 "Pills (status)") at the right of its first line, centred on it: Symlink, or Library when it leads into the library. Escape, its trigger or a click outside closes it. The menu is not modal: the page under it stays live, so a click outside also acts on what it hits, as with a popover. A modal menu makes the whole page inert while it is open, which costs a style recalculation of every element when it opens and again when it closes, so it lags on a long list. No animation.

### Button group

shadcn's Button Group (`src/components/ui/button-group.tsx`): ghost buttons side by side with no gap, read as one filled shape rounded on its outer corners only, with a 1px separator between them that has no fill, so the surface behind the group shows through as a thin dark line. An action with a menu of related actions (`src/components/split-button.tsx`) is the action's button, the separator, then a square icon button with the `MoreIcon` that opens the menu (§6 "Menu"); the trigger has its own accessible name, Enter, Space and ArrowDown open the menu, and Escape closes it and returns the focus to the trigger. The buttons keep their own sizes, as shadcn's do: a labelled action is a small button (`sm`) with a square `icon-sm` trigger, as Add in the Skills header; an icon action is an `icon` button with an `icon` trigger, as Show in Finder on a file row. Only the action shows when there is nothing to choose.

### Skills header

The Skills list column's header: the page title and the skill count on the left; on the right, `button.gap` apart, Check updates and Add, a button group whose action opens the Add skill dialog and whose menu opens the Create skill dialog (§6 "Button group"). The menu holds Create skill alone, as Sources sits under Skills in the sidebar. Check updates is a ghost `icon-sm` button with the refresh icon, as tall as the group, named by its label, which its tooltip shows (§6 "Tooltip"); while the check runs it is pending (§6): `aria-busy`, with a `Spinner` in the refresh icon's place until the check ends, in the same square and ghost fill. Its progress is a toast with Cancel; when the check ends with an update the app offers, the toast goes and the Update skills dialog opens (§6 "Update skills dialog"), else a toast says there is none. Icon-only keeps the header controls inside the header's right padding, ending where the filter row ends, with room for a three-digit count at every window size, down to the minimum window.

### Skills list

The rows of the Skills list column, in their groups (§6 "Group header"). Only the headers and rows near the part of the list that shows are in the document, so a machine with hundreds of skills scrolls as one with a dozen. The list places each drawn header and row at its own offset, with the heights and `row.spacing` the tokens give; the library that does this (`@tanstack/react-virtual`) writes the offsets and the list's height on the elements itself. A group's label carries its full count, and each row its place in its group (`aria-posinset`, `aria-setsize`). The selected row comes into view below the pinned headers when the selection changes: by a key, a click, or a route another screen opened.

While the list scrolls, the header of the group that shows at the top stays pinned there with `position: sticky`, on `shell.content-bg`, and in PLUGINS the header of the plugin pins `group.header-height` under it. Each stays pinned while its group or plugin shows; the next header pushes it out, and a plugin's header slides under its group's. A click on a pinned plugin's header collapses or expands it and keeps it at the top.

### Tooltip

shadcn's Tooltip (`src/components/ui/tooltip.tsx`) names an icon-only button for the pointer, shows the full text of a shortened label, such as a source group's URL, or says why an inert agent chip does nothing: `tooltip.radius`, `overlay.bg` with 1px `overlay.border`, one `type.secondary` `text.primary` line with shadcn's padding, `tooltip.offset` from its trigger, on the side with room. It opens at once on hover or focus, as shadcn's provider sets, and closes on leave, Escape or a click. No arrow and no animation, like the app's other overlays. The button keeps its own accessible name; the tooltip adds no text a screen reader needs, but for an inert agent chip, whose reason it describes, and a help term.

A help term (`src/components/help-term.tsx`) is a word in running text that explains itself: it keeps the text's type and colour with a dotted underline and the help cursor, and is a button that does nothing on a click, so its tooltip opens from the keyboard too. The punctuation that follows it sits inside the button, outside the underline, so a screen reader reads no space before it. Its tooltip, and the Universal pill's, take `size="text"`: the sentence wraps past `tooltip.text-max-width`.

### Hover card

shadcn's Hover Card (`src/components/ui/hover-card.tsx`) shows rich content, such as the table of what each agent sees, while the pointer rests on a word in running text. Its frame is the popover's (§6 "Popover (Filter)"): `popover.width`, `popover.radius`, `overlay.bg` with 1px `overlay.border` and `popover.shadow`, 14px padding (none at the top and bottom for a list that scrolls to the card's edges, `padding="list"`, which also fits the card to the list's width), `popover.gap` between its blocks, `popover.offset` below its trigger, at most `hover-card.max-height` high and scrolling past it. It opens `hover-card.open-delay` after the pointer reaches the trigger, so a pointer passing over the text opens nothing, and closes `hover-card.close-delay` after it leaves the trigger and the card, so the pointer can cross the gap into it. Escape closes it. Its trigger is a word with a dashed underline, which a dotted help term (§6 "Tooltip") does not take, so the two read apart. No arrow and no animation.

### Popover (Filter)

`popover.width`, `popover.radius`, `overlay.bg` with 1px `overlay.border` and `popover.shadow`, 14px padding, `popover.gap` between groups, `popover.offset` below its anchor. Each group opens with a caption in `popover.caption`: a row of choice pills that wraps (the Toggle Group of §6 "Segmented control", `kind="pill"`: `type.secondary`, count `type.technical-sm` `text.caption`; the chosen one `button.secondary-bg`, `text.primary`), a segmented control, and agent rows of `popover.row-radius` with the round checkbox, a 20px logo and the name. A ghost reset shows while a filter is applied. A click outside or Escape closes it.

- The popover never leaves the window: it is at most `popover.max-height` high and scrolls inside that.
- AGENTS lists the checked agents first, then the rest by the popularity of the agent client (`src/lib/agent-popularity.ts`), Universal where its most used client would stand; the order is fixed when the popover opens. A machine can have dozens of agent configurations, so the group shows six rows, plus every row that is checked or was since the popover opened, and a small ghost button under them that shows the rest and hides them again. With seven rows or fewer it shows them all and no button.

### Sheet

Every sheet (History, What's new, Activity log) has the same frame and the same default width.

- `sheet.width`, `sheet.inset` from the top, right and bottom, `sheet.bg` with 1px `sheet.border`, `sheet.radius`, `overlay.shadow`, `sheet.padding`, over `shell.scrim`. Title `type.dialog-title`, the subject's name under it in `type.technical` `text.caption`, a ghost close at the top right; Escape closes it too.
- A `sheet.handle-width` handle on its left edge drags its width between `sheet.min-width` and `sheet.max-width`. The handle shows `sheet.handle` on hover and while it drags, and `shell.focus-ring` on focus. A double click or Home brings back the default. Each sheet keeps the width the user left it at in localStorage, so it opens at that width after the app restarts too.

### Sheet (History)

The sheet frame (§6 "Sheet"). Content scrolls down only: a long line wraps, never clips or scrolls sideways. Each entry is a Collapsible (`src/components/ui/collapsible.tsx`) whose trigger is a plain inline button (no fill, no padding, no radius): a chevron and the subject, which expands it to the files its commit changed, one row each: status and path in `type.technical-sm`, `text.caption` and `text.secondary`. The chevron hangs outside the entry: the meta line and the files start under the subject, `timeline.indent` in. A 1px `timeline.connector` runs from under each chevron, 4px below its line, to 4px above the next entry's chevron, so the entries read as one history. An entry whose subject holds a link (§6 "Timeline (history)") keeps the chevron alone in its button, and the subject follows it.

### Sheet (What's new)

The sheet frame (§6 "Sheet"), for what an update changes before the user takes it. Opened from the Update skills dialog, it and its scrim take the dialog's layer, later in the document, so they sit above the dialog.

- An open file's pane is as high as its lines, with no `code.max-height`: no empty pane under them. When the blocks overflow the sheet, a patch with more lines than fit in `code.min-height` gives up height, in proportion to its own, down to `file.open-min-height`, and scrolls inside; a shorter one keeps all its lines. Past that, the sheet's content scrolls. The card's items lay out in the sheet's column, each drawing its part of the card's border.
- While the changes are read, every file the update check lists has its row, open unless the user closed it, over the loading pane of the File list (§6 "File list"), as high as its stand-in, with the spinner in the first one only. When the patches come in, the stand-ins fade out over them, so the blocks keep their place.
- A patch with no lines to show and a failed read each say so in `type.secondary` `text.hint`; a failed read adds the CLI's message in `type.technical-sm` `text.caption` and raises no error toast.
- An update from another machine cannot be diffed before it is taken: the sheet lists its files in the same card, status and path, with no chevron.

### Sheet (Activity log)

The sheet frame (§6 "Sheet") (`src/app/activity-log.tsx`): the last lines of what the app ran and what the background scan reported, as a terminal shows them. A sheet, since the log is looked at beside the screen whose action it explains, and from any screen; Settings and the palette open it.

- One block of lines, oldest first: `code.bg` with 1px `code.border`, `log.output-radius`, `type.technical-sm`, long lines wrapped. A line is its time in a `log.time-width` column, its text, and a note at the right: how long a command ran, and how it ended when it did not succeed. No rows to open, no dots and no filter: the log is read as a terminal is.
- Red is the one colour, and it means failed. A command that failed is `tone.danger.fg` with its last 5 lines of output under it, in the same colour and with no time. A command that succeeded keeps its line only, in `text.secondary` with its time and note in `text.caption`. What the background scan reported and a command's debug lines are `text.caption`, so the commands stand out. A scan the app restarts on purpose is no failure.
- The view stays at the end while lines arrive, and a view the user scrolled up stays where it is.
- The log keeps a fixed number of lines, each cut at a fixed length, and a line beyond the limit removes the oldest one: the app runs for days, and the log takes the same memory throughout. Under the block, one `type.technical-sm` `text.caption` line counts the lines kept and dropped, and at the right the failures, `tone.danger.fg` when there is one.
- Under the header, a switch row (§6 "Switch") that makes later commands and the serve child log at debug level. Off by default: debug lines cost an event per git call and fill the serve log's 25 MB within a few scans of a large library, so the user turns them on to look into one action. A command's debug lines go under its line; the scan's stay in its log file.
- Above the block, two small ghost buttons `button.gap` apart: one copies the lines as plain text, one shows the serve log file in the file manager. The sheet never reads that file: it is large, and a text editor reads it better.
- The log shows text made safe when it is recorded: a URL's credentials replaced, an inventory as counts, a file's content left out, long values and lines cut.

### Code pane

`code.bg`, `type.code`, grid `code.gutter 1fr`: line number in `code.line-number`, right-aligned · marker · text. Front matter and keys `code.frontmatter`, headings `code.heading`, list items `code.list`, prose `code.prose`. Diff tints: an added line on `diff.added-bg`, a removed line on `diff.removed-bg` with no number. On a tinted line, front matter takes `code.prose` so it keeps 4.5:1. The text is selectable, so it can be copied; the line numbers and markers are not, so a copy holds only the file's text.

- Changed text: within a change, the text that differs between its removed lines and its added lines is marked as VS Code's diff editor marks it, with VS Code's own diff (copied into `src/vendor/vscode-diff`): a character diff of the whole change, extended to whole words where most of a word changed, with short kept runs between two changes joined into them. The changed text sits on `diff.removed-word` and `diff.added-word` in `code.strong`, since a line's own tone falls under 4.5:1 on those tints. White space counts, so a change of indentation is marked.
- A change with one side, only removed or only added lines, has its line tint alone. So does a change that takes longer than 100 ms to compare.

### Inline Markdown

A skill's description is one line of Markdown from its SKILL.md, so wherever it shows (a Skills row, the detail header, the Sources rows, the Add skill dialog) it renders its inline Markdown with `InlineMarkdown` (`src/components/inline-markdown.tsx`), read by marked's inline lexer. It renders inline elements only, so the line keeps its own type, colour, truncation or line clamp. A code span is `type.markdown-code` in `markdown.code-text` on `markdown.code-bg`, `markdown.code-radius`, 4px side padding; strong text is 600 in `markdown.strong`; emphasis is italic and strikethrough is struck. A link shows its text, as the line sits in a row that opens on a click. Block syntax stays as written. Accessible names and descriptions use the text without its Markdown (`markdownText`).

### File list (detail pane FILES)

A `card.bg` card, `file-card.radius`, 1px `card.border`, one row per file: `file-row.height`, `file-row.padding-x`, a 1px `file-row.divider` between rows, `file-row.hover` on hover or focus.

- A row: a chevron (turns 90° when open), the path in `type.technical` (`file-row.path`, `file-row.path-open` while open), a `file-row.edited-label` status for a file the user changed, and the meta (line count, binary, or size) in `file-row.meta` at the right. An open row shows the file in the Code pane below it.
- SKILL.md comes first and open; the rest follow by path, a file the user removed among them.
- While the files are read, the card shows the SKILL.md row alone, open and with no meta, over a Code pane at `code.height` that holds a stand-in SKILL.md, blurred by `code.loading-blur` like frosted glass, with a `md` (20px) `Spinner` in `text.muted` at its centre. When the files come in, the stand-in fades out over the real SKILL.md, which fades in from the same blur to sharp (§8). Every skill has a SKILL.md, so the list keeps its place when the files come in. The stand-in is hidden from assistive technology; the spinner's status says the files are being read.
- A file on disk has a Show in Finder button group (§6 "Button group") over the meta, beside the trigger, never inside it. While the row is hovered, focused or has its menu open, the meta fades out and the button fades in at the same spot. The meta stays laid out, so the row never shifts, and the button stays in the tab order throughout. A removed file has no button.
- The button opens the file at the skill's first place: the library when it holds the skill, else the first placement in the row's agent order. The chevron's menu lists every other placement, the path as the inventory carries it. A skill with one place has no chevron.

### Key hint

A key or key combination such as `esc` or `⌘K` (`KbdGroup` and `Kbd`, `src/components/ui/kbd.tsx`): `KbdGroup` is the hint, `key-hint.padding`, `key-hint.radius`, `button.secondary-bg`, `type.key-hint` `text.muted`. Each key of a combination is its own `Kbd` in it, `key-hint.gap` apart, so `⌘K` and `Ctrl K` space their keys the same way. `⌘` is the `3xs` command-key icon (§7), since the mono font lacks the glyph and each system draws its fallback at its own size and height.

### Inputs

`input.height`, `input.bg`, `input.border`, `input.radius`, padding 0 `input.padding-x`, `type.input`; focus `input.border-focus`. `type.input-mono` for URLs and commands. Search fields are shadcn's Input Group (`src/components/ui/input-group.tsx`): the magnifier on the left, in `input.placeholder` like the placeholder. List search field (Input `list-search`): `input.search-height`, `input.search-radius`, `input.search-padding`, `type.secondary`, the `xs` magnifier. Sources search (Input `search`): `add-search.height`, the same radius and padding, `type.input`, the `sm` magnifier.

### Settings

The Settings page (`src/screens/settings`): the serif page title, then one section per area, a `shell.divider` hairline between sections and none above the first.

- A section is a `settings.columns` grid with `settings.column-gap` and `settings.section-padding` above and below. The label column holds the label in `type.settings-label` `text.primary` and one line under it in `type.secondary` `text.muted`; the content column holds the rows, `settings.section-gap` apart.
- Inputs are `input.height`. Paths and ids are `type.technical`, ids in `text.hint`.
- A switch row (`src/screens/settings/switch-row.tsx`) is the setting's name in `type.body` `text.primary` over its line in `type.secondary` `text.muted`, the switch on the right (§6 "Switch"). A setting that cannot change now keeps its row: the switch is disabled, and the line says what it needs first.
- A failure under a field is a refusal under a field (§6 "Refusal under a field").
- Editor holds an app preference, not a machine setting (ADR 0013): it runs no command and saves the moment it changes. A radio group (§6 "Radio group") lists the editors found on this machine, and one chosen before that is gone since, so the choice still shows. A custom command is a mono input under the group, saved on Enter or when it loses focus, with a hint under it in `type.secondary` `text.hint`.
- Hub, once set, shows its URL in `type.technical` with a neutral Read-only pill beside it when access is limited, as a source card does (§6 "Source card"); a remote you can write to has no tag.
- Import skills sits after Hub: one secondary button per tool with skills to adopt, each opening the adopt dialog (§6 "Adopt dialog") for that tool. With none, one line in `text.muted` keeps the section in place.
- A choice that replaces something (an existing snapshot file on Save snapshot, the machine's settings on Apply snapshot) opens a confirm dialog with a danger confirm; a picked file's path shows in `type.technical` `text.caption`.
- The result of Apply snapshot is one `type.secondary` line per outcome: what it installed or updated in `text.secondary`, then the warnings in `tone.attention.fg` after an `AlertTriangleIcon` (`xs`). A skipped skill's name has a dotted underline and the CLI's reason as its tooltip. When nothing changed, the one line is `text.muted`.

### Publish status (title bar)

At the right end of the title bar (`src/app/publish-status.tsx`, drawn by `src/components/status-button.tsx`): a pill `shell.status-height` high, `shell.status-radius`, with the ghost button's fill (`button.ghost-bg`, hover `button.ghost-hover` with the label in `text.secondary`), since it is a button and every button has a resting shape. An 8px dot (`shell.status-dot-radius`) in a 16px box, then the label in `type.status` `shell.topbar-text`. It shows how this machine stands with the hub, and never runs a check: serve checks on its own, and Check updates in the Skills header (§6 "Skills header") is the one check by hand.

- States, from the inventory's library, the Sources slice and the update candidates: no hub (dot `text.hint`; goes to Settings); something to publish, incoming, or both, the two counts side by side (`shell.status-pending` with `shell.status-pending-halo`); and synced, nothing either way (`shell.status-published` with `shell.status-published-halo`). Every state but the first opens the popover.
- To publish counts hub skills edited since they were published and those never published, with the predicates the Skills screen's banners use (`hasEditsToPublish` and `notPublished` in `src/screens/skills/banners.ts`), so the title bar and the Skills screen never disagree. Incoming counts the update candidates of hub skills that another machine published (`fromAnotherMachine`) to the hub set now, for skills still in the library. Serve's own checks add to the candidates, and Check updates replaces them.
- A tooltip (§6 "Tooltip"), aligned to the pill's end, says what the counts hold, one line for each, or for synced, when the hub was last checked. The pill keeps its label as its accessible name. No tooltip while the popover is open or a publish runs.
- Beside the pill, never inside it, a visually hidden polite live region (an `output`, whose role is `status`: `StatusAnnouncement`) holds the summary label, so a screen reader announces a change a background check brings. It never holds the publish's progress.
- The popover (§6 "Popover (Filter)" for its frame, `popover.padding` for grouped content) opens below the pill, aligned to its end. A group for each count, with its caption in `popover.caption`: rows of `popover.row-radius` with the skill's name in `type.secondary` `text.primary` and what the group says of it in `text.muted` at the right. Every row is a button, `popover.row-hover` on hover, that opens the skill and leaves the popover open, so the next row is one click away; an incoming skill's banner offers Update. To publish says edited or new for each row and ends with a small primary Publish all at the group's right, as every call to action in the app, which publishes every row in one command and closes the popover. With nothing either way, one `type.secondary` `text.muted` line says so. The footer names the hub in its short form, `type.technical-sm` `text.caption`, and when it was last checked at the right, `type.secondary` `text.hint`. The remote never truncates: the popover is at least `popover.width` and widens to fit its content, up to `popover.max-width`; in a window narrower than that, the time wraps under the remote. The popover holds no check.
- Last checked is when serve's last update check ended well, or Check updates reached every source or all but some; until either, the popover and tooltip say it was not checked yet. It reads as a relative time that keeps time while the app is open, once a minute.
- While the publish runs, and until the inventory that follows arrives, the label counts its progress and the pill is `aria-busy`; so is Publish all, with a `Spinner`. The run's progress also runs on the pill's rim, the `sm` progress rim (§8 "Progress rim").
- Hidden while the startup layer shows, and until the inventory and the sources are read, so it never guesses a state.

### Sidebar

The nav items in sidebar order, `shell.nav-gap` apart. A screen nested under another, Sources under Skills, sits under its parent, indented to its parent's icon, behind a 1px `shell.nav-tree` rule, 32 high. A parent is active on its own routes and not on its nested screen's: Skills is active on a skill and on Update review, Sources alone on its page.

While the last update check found an update the app offers (one with no pending merge), the Skills item shows the update pill (§6 "Pills (status)") at its right end, 8 in and centred on it. The pill is a sibling of the link laid over it, never inside it, so a click opens the Update skills dialog (§6 "Update skills dialog") and goes nowhere. It goes once no update is left.

### Sources screen

The Sources screen (`src/screens/sources`), under Skills: the serif page title with Refresh all and Add source, the search field, the source pills, the card of the open source, then the select-all row and the skill list.

- The header: the page title, and at its right, `button.gap` apart, Refresh all and Add source, both secondary. Refresh all runs one `source fetch --all`, then reads Hub's skills again (`skill list --remote`) when there is a hub; until both end it is pending (§6): `aria-busy` and disabled, with a `Spinner` in the refresh icon's place, so it keeps its size. It is disabled while there is no source.
- A source pill (`source-pill.*`, `pill.radius`) holds the attention dot of a stale source, the source's short name, and its skill count in `type.technical-sm` (`source-pill.count`, `source-pill.active-count` on the active pill) once it is known.
- The card of the open source: `source-card.bg`, a 1px `source-card.border`, `source-card.radius`. Its top row is a 36px owner tile (`source-card.tile-*`, `source-card.tile-radius`, the owner's initial in `type.source-name`), the name in `type.source-name`, then a neutral Read-only pill when access is limited, and under them the full URL with its `#pin` in `type.technical-sm` `text.muted`, a link that opens the repository in the browser, underlined on hover; at its right, Refresh (secondary) and Remove (danger), `button.gap` apart. Under it, after a 1px `source-card.divider` rule, a strip of four facts, each a `type.secondary` `source-card.fact-label` label over a `type.technical-sm` value in `source-card.fact-value`, a never-fetched source's Fetched in `tone.attention.fg`. The name, the URL and the facts' values are selectable, so they can be copied; a drag across the URL selects it and opens nothing. The card shows tags only for limits: a source you can write to has none. It hides while a search runs, since a search looks across every source; Hub's card has no Remove, since Settings manages the hub.
- The select-all row: one checkbox (§6 "Checkbox (popover)"), checked, mixed or not as the selection is, its label in `type.secondary` `text.secondary` counting the selection. In Sources it sits at the right: the label, then the checkbox, centred over the round checks of the cards. The Add skill and Update skills dialogs keep the checkbox first, at the left, over their column of checkboxes.
- The skill list: one column, a card per skill, `add-card.spacing` apart, A to Z by name whatever their state, so a card keeps its place when its skill is added or removed. A search answer groups them by source, each group under its source's `type.caption` `group.label` caption. Each card is an accordion item (`src/components/ui/accordion.tsx`, variant `list`): `card.bg`, a 1px `card.border`, `add-card.radius`, `add-card.hover-border` on hover; a selected skill's card `add-card.selected-bg` and `add-card.selected-border`.
- A closed card is one row of `file-row.height`, `file-row.padding-x`: a chevron in `file-row.meta` (turns 90° when open, as in the File list), the name in `type.row-name` `text.primary`, then one line of the description in `type.row-desc` `text.muted`, both truncated, the description filling the rest of the row. At the end, the round check: a 20px ring in `add-card.check-ring`, filled with `add-card.check-bg` and an `on-accent` check when selected, in a 32px hit area (`add-card.check-radius`). A skill this machine holds already has its status there instead, in `type.secondary` 500: Added (`add-card.status-added`), Forked (`add-card.status-forked`), Name taken (`add-card.status-taken`) when another skill holds its name. When this machine recorded it in the inventory's installs (a source's skill by its source and subpath, a hub skill by its fork id), Added reads Added on and the date its skill was last added, all in `add-card.status-added`, and a card that can be selected shows the date its skill was last removed before the check, in `type.technical-sm` `file-row.meta`, as the row's meta, so the check keeps its place. An open Name taken card says under its row what holds the name and how to free it, for example "Your library has xlsx skill from zeta/tools. Remove or rename it to add this skill.", the name of that library skill a `text.primary` underlined link that opens it on the Skills screen.
- Added is a button (`src/components/remove-swap.tsx`) in a 32px high hit area that ends where the check's ring would. While it is hovered or focused, Added hides at once and Remove fades in over 150ms in its place, in `type.secondary` 500 `add-card.status-remove`, with no fill; both words share one cell, so nothing moves. Remove opens the Remove dialog of Skills (§6 "Banner actions and dialogs"), which lists every agent that sees the skill; its Remove runs `skill remove <name>`, which takes the skill out of the library and every placement agentx made, so universal and the other agents lose it at once. A copy of the same name that agentx did not place stays. Remove stays shown, disabled, until the inventory after the command arrives.
- The row opens and closes on a click anywhere but the check, and several can be open at once; only the check selects. The row and the check each draw the focus outline inside the card. A row whose SKILL.md is not read yet stays closed while it is read: a `sm` `Spinner` in `text.muted` takes the description's place, with no text, as the read is often too quick to read it; its tooltip (§6 "Tooltip") and its status say Loading skill…, and the row is `aria-busy`; it opens when the read ends, so the card grows once, to its full content. A click while it waits cancels the open. While open, the row's description line hides, since the frontmatter of SKILL.md holds it, and See full skill sits beside the name in the same row, on the name's baseline, a link in `type.secondary` `text.primary`, medium, underlined, with the `2xs` external-link icon after it, which opens the skill's directory on its source's web page at the commit the source was fetched at (GitHub's `/tree/` page, GitLab's `/-/tree/`; no link for another host). The trigger lies under the whole row, so the link takes its own clicks and every other click opens or closes the row. The list keeps its scroll as a row opens, so the content shows below the row, below the fold if need be. Under the row come, for a skill that cannot be selected, why in `type.secondary` `text.hint`, wrapped, `file-row.padding-x` in then the skill's SKILL.md in the Code pane, edge to edge. When it cannot be read, one `type.secondary` `text.hint` line on `code.bg` takes its place. Your skills on the hub have no SKILL.md to read here: they open at once to their description alone.
- Only the cards near what shows are in the document, so a source of thousands of skills stays quick; the list scrolls with the screen, and an open card takes its measured height.
- Remove opens a dialog (§6 "Dialog") with no line under its title. When this machine has skills from the source, a checkbox offers to remove them too, unticked, their names listed under it in `type.technical` `text.muted`; the confirm is a danger button.

### Command palette

The ⌘K palette (`src/app/command-palette.tsx`): ACTIONS, GO TO, SKILLS, in that order, each under its caption. A row is its icon in `palette.icon`, its label in `type.body` and an optional hint in `type.palette-hint`; typing keeps the rows whose label holds the text.

- ACTIONS shows an action only when it has something to act on: Publish all while the publish status counts skills to publish, for the same skills as the status's Publish all. The palette's check for updates is Check updates of the Skills header (§6 "Skills header"). The action that opens the activity log is last.
- SKILLS lists every skill in the Skills list's order, its origin as the hint; while nothing is typed, only the first four, so the actions and screens stay in view.

## 7. Iconography

Stroke icons on a 24 grid, `stroke-width` 1.75–2.25, round caps and joins. Fill only for the dot glyph. No icon fonts, no emoji.

An icon is a hand-written React component that takes the text colour; an illustration (such as the agentx mark) is an SVG file in `src/assets` shown as `<img>`.

Sizes: `3xs` 10px (in a key hint), `2xs` 12px (in a pill), `xs` 14px, `sm` 15px, `md` 16px (the default), `lg` 18px.

The product mark is `shell.logo-mark` on `shell.logo-bg` with a 1px `shell.logo-ring` ring: 30px with `shell.logo-radius` in the sidebar (the height of the product name and machine label beside it), 40px with `startup.logo-radius` on the startup layer. Its SVG is also the source of the app icons: after changing it, run `pnpm tauri icon src/assets/agentx-mark.svg` and keep only the files `src-tauri/tauri.conf.json` lists. The SVG holds the two colours as hex values (`lime-400`, `gray-990`); change them together with the tokens.

## 8. Motion

Nothing loops forever, and nothing waits on purpose. Two things loop, each only while something runs. The `Spinner` of a pending control (§6), of a running check, or of a read the screen waits on, such as a skill's files, turns only while its command runs and stops when the command ends. A progress rim (below), on the Add skills bar or the sync pill, draws only while its command runs, and stops once it has faded.

- Hover and background changes: no transition, or at most `.2s`.
- Switches: the track changes colour in `.2s`; the knob moves with no transition.
- File list chevron: `rotate(90deg)` in `.15s`. The History sheet's chevron turns with no transition.
- File row: the meta and the Show in Finder button swap by opacity in `.15s`.
- Files read (`animate-code-reveal`, `animate-code-loading-out`): when a skill's files come in, the blurred stand-in SKILL.md and its spinner fade out while the real SKILL.md fades in, its blur going from `code.loading-blur` to none, both `.3s ease-out`. A skill whose files are already read shows them at once.
- Dialogs and the palette: shadcn's own open and close classes (tw-animate-css). The content fades and zooms from 95% in `.2s`, and back the same way when it closes; its `shell.scrim-strong` fades in and out in `.15s`. Sheets open and close with no transition.
- Popovers (the sync status, the Filter popovers): shadcn's own open and close classes. The popover fades in and zooms from 95% at `popover.origin`, sliding 8px in from its trigger's side, in `.15s`; it fades and zooms out the same way when it closes, and takes no clicks on the way out.
- Floating bar (`animate-floating-bar`, `animate-floating-bar-out`): it slides up from below the window's bottom edge (its height plus `floating-bar.offset`) as it fades in, `.28s` ease-out, when it appears. Once nothing is selected and no install runs, it slides back down below the edge as it fades out, `.22s` ease-in, still showing what it last showed, and takes no clicks on the way out (`src/screens/sources/action-bar.tsx`, `FloatingActionBar`). Selected again before it is gone, it stays.
- Progress rim (`src/components/progress-rim/progress-rim.tsx`, `rim-progress.ts`, `rim-shader.ts`): the progress of a long command on the rim of a pill. While `skill add` runs, two arcs of light grow on the floating bar's border from its bottom centre, up each side, and meet at the top. A WebGL shader draws them on a canvas 26px larger than the bar on each side: a thin line in `floating-bar.rim` with `floating-bar.rim-flow` drifting through it, a soft glow outward that breathes, light that is faint at the tail and brightest at the head, and a 42px soft leading edge. The rim fades in over `.5s`. It never shows the CLI's steps as they come, since they arrive in bursts (three per skill and a rescan): it creeps toward 90% on its own, as a browser's loading bar does, and the steps only raise the floor it eases toward, at most 16% a second, so it never jumps and never reads as done before the run is. When the run ends, the end takes `1.79s`, and each phase is a share of it, so the whole end gets longer or shorter at once: the rim eases into 100% over the first 67% and the two arcs join from 33.5% to 83.8%; from 67% to 90.5% a bright band sweeps from the top centre back down both sides (smootherstep), its light rising fast and falling away; from 92.2% the rim fades out (smootherstep). Only then does the bar close if no skill is selected. A stopped or failed run drains back toward the bottom centre fast (`8` a second) and fades as it goes: it takes `.31s`, and fades from 16% of it (smootherstep). Reduced motion turns the sweep into one brief brightening of the whole rim. Reduced motion keeps the light still; the progress itself still grows. Without WebGL nothing is drawn and the bar keeps the same timing. The same rim, `sm`, runs on the title bar's sync pill while Publish all runs (§6 "Publish status (title bar)"): a canvas 14px larger than the pill, with a glow and head at 0.45 of the size and 0.55 of the brightness, so it reads as a hint beside the label, which still counts the skills. On so small a pill the joined ring would outshine the sweep, so as its arcs join the ring turns faint (its line at 0.45 of the light and 0.8 of the width, its glow at 0.15) and the band, unchanged, stands out from it.
- Startup check running (`animate-check-spin`): the status ring turns once a second, linear, until the check settles.
- Pending controls (`Spinner` in `src/components/ui/spinner.tsx`, `animate-check-spin`): the same ring, once a second, linear, for the whole command; a control with a leading icon, such as Check updates or Check tools, shows it in the icon's place, so a long check reads as busy past its first second.
- Screens fade in (`animate-fade-in`): opacity 0 → 1 over `.2s ease-out` when they mount, once the startup checks pass; a passing launch shows no startup layer.
- A launch with a kept inventory shows its screens with the first frame, with no fade, under a frosted pane: `shell.loading-veil` with a `shell.panel-loading-blur` backdrop blur, and on top the launch progress of an empty panel (the `Spinner`, Starting the app…, and from 5 seconds how much longer the check keeps trying). The pane takes every click and key, the screen under it is inert, and a screen's own shortcuts, such as the arrows of the skill list, wait for the checks, so nothing in the main panel starts before they pass. The sidebar, the command field and the sync status look as always but are inert, and ⌘K opens nothing, until the checks pass; the title bar still drags the window. This holds on a launch with no kept inventory too. When the checks pass, the pane fades out in `.3s` (`animate-code-loading-out`).
- A launch with no kept inventory shows, from its first frame, a `Spinner` and a status line (`type.secondary`, `text.muted`) in the empty panel, faded in the same way. The line says only that the app loads, not which check runs. From 5s into a check, a countdown line under it (`type.row-desc`, `text.hint`).
- Diff pane choice: `opacity .2s, box-shadow .2s`.
- Hover card: `hover-card.open-delay` .2s after the pointer reaches its trigger, so a pointer passing over the text opens nothing; `hover-card.close-delay` .3s after it leaves, so it can cross into the card. Each is a number passed to the hover card (`openDelay`, `closeDelay`). It appears and goes with no transition.
- Button enable and disable, every variant: `opacity .2s`.
