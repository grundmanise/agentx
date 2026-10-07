# agentx desktop: design system

The visual contract for the desktop app. Code uses the semantic tokens below, never a primitive or a raw value. The tokens live in `src/styles/tokens.css`, which is edited by hand and is the source of truth; this file documents them. When you add or change a token, edit `tokens.css` and the matching table here in the same change. The tables use dotted names, and `tokens.css` names them like this:

- A colour token `shell.bg` is `--color-shell-bg`, a Tailwind colour (`bg-shell-bg`, `text-text-muted`). Primitives are `--gray-990` and so on, outside the Tailwind theme, so no utility class can name one.
- A tone `tone.primary` is `--color-tone-primary-fg`, `-bg` and `-border`.
- A shadow token `overlay.shadow` is `--shadow-overlay` (`shadow-overlay`).
- A radius token `card.tile-radius` is `--radius-card-tile` (`rounded-card-tile`).
- A layout token with one value `shell.sidebar-width` is `--shell-sidebar-width` (`w-(--shell-sidebar-width)`). A token given as a range (`38–40`) is guidance, not a variable; pick the value the component calls for.
- A type token `type.page-title` is the utility `type-page-title`, which sets the face, size, weight, line height, tracking and case together.

The Tailwind theme holds only these tokens. `tokens.css` resets Tailwind's whole default theme (`--*: initial`), so no default colour, spacing step, radius, shadow, font, font size, weight, line height, tracking, breakpoint or animation exists. Besides the tokens above, the theme has three font weights (`font-normal`, `font-medium`, `font-semibold`) and the spacing steps of §4: a 2px grid in half steps from `0` to `12` (48px), with no base `--spacing`, so `p-13` or `top-28` produce no CSS. `pnpm lint` rejects any class Tailwind does not know and any arbitrary value (`text-[13px]`). When a design needs a value no token holds, add the token to `tokens.css` and to the table here; see the README's rules for components.

## 1. Character

A dark, quiet desktop tool. One accent (lime) carries the accent and action signals; everything else is near-black and grey. Serif for page titles only; a mono face for anything technical (paths, ids, dates, captions). Surfaces are flat: elevation comes from one shade step and, on floating layers, a soft black shadow. No gradients, no emoji, no icon-per-row decoration.

Copy voice: short, plain, present tense.

## 2. Color

Two layers. **Primitives** are the raw palette and never appear in component code. **Semantic tokens** name a role and point at one primitive; everything in §3–§8 and in the app's code uses semantic names only. Implement as CSS custom properties (`--color-shell-bg: var(--gray-990)`), Tailwind theme colors, or a token file; the names below are the contract.

### 2.1 Primitives

Grey ramp (dark → light):

| Name | Hex |
|---|---|
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
| `gray-500` | `#5d5d66` |
| `gray-400` | `#6e6e76` |
| `gray-300` | `#8b8b93` |
| `gray-250` | `#a0a0a8` |
| `gray-200` | `#b4b4bb` |
| `gray-150` | `#c9c9cf` |
| `gray-100` | `#dcdce0` |
| `gray-50` | `#ededef` |

Hues:

| Name | Hex |
|---|---|
| `lime-400` | `#d4ff4f` |
| `lime-300` | `#e2ff7a` |
| `amber-400` | `#ffc15e` |
| `red-400` | `#ff7b72` |
| `violet-400` | `#b69cff` |
| `blue-400` | `#7cb7ff` |
| `white` | `#ffffff` |
| `black` | `#000000` |

Alpha is written as `<primitive>/<percent>` and resolves to `rgba` of that primitive (e.g. `lime-400/12` → `rgba(212,255,79,.12)`, `black/55` → `rgba(0,0,0,.55)`).

### 2.2 Semantic tokens

Grouped by element. Each token points at one primitive (or at a meaning token when the value is shared by design). §4–§6 and the app's code use these names only; two elements with a look-alike value still get their own token so one can change without the other.

#### Meaning (accents and tones)
| Token | Primitive | Meaning |
|---|---|---|
| `accent.primary` | `lime-400` | Primary action, enabled, update available, success, selection |
| `accent.primary-hover` | `lime-300` | Primary button hover |
| `accent.attention` | `amber-400` | Edited, displaced, needs a look but not broken |
| `accent.danger` | `red-400` | Removed, unreachable, destructive |
| `accent.yours` | `violet-400` | Your own skills, "Yours" side of a diff, publish |
| `accent.theirs` | `blue-400` | "Original / update" side of a diff |
| `on-accent` | `gray-990` | Text and icons on any accent or inverse fill |

Tone triples `{ fg, bg, border }` for banners and Inbox icon tiles:

| Tone | fg | bg | border |
|---|---|---|---|
| `tone.primary` | `accent.primary` | `lime-400/8` | `lime-400/25` |
| `tone.attention` | `accent.attention` | `amber-400/8` | `amber-400/25` |
| `tone.danger` | `accent.danger` | `red-400/8` | `red-400/25` |
| `tone.yours` | `accent.yours` | `violet-400/8` | `violet-400/25` |
| `tone.neutral` | `gray-300` | `gray-910` | `gray-800` |

#### Text (shared across elements)
| Token | Primitive | Element |
|---|---|---|
| `text.primary` | `gray-50` | Titles, body, values |
| `text.secondary` | `gray-200` | Secondary text, Details values |
| `text.body` | `gray-150` | Card copy, palette rows |
| `text.muted` | `gray-300` | Descriptions, banner text |
| `text.hint` | `gray-400` | Helper copy, Details keys, "+n" counts |
| `text.caption` | `gray-500` | Uppercase mono captions, chevrons, line counts |
| `text.disabled` | `gray-600` | Disabled glyphs, inactive dots, counts |
| `text.faint` | `gray-700` | Line numbers, hollow timeline ring |

#### Window and shell
| Token | Primitive | Element |
|---|---|---|
| `shell.bg` | `gray-990` | Window body, sidebar, startup layer |
| `shell.content-bg` | `gray-970` | Content area, both split columns |
| `shell.divider` | `gray-890` | Split column divider, section dividers, table header rule |
| `shell.scrim` | `black/50` | Behind the history sheet |
| `shell.scrim-strong` | `black/55` | Behind dialogs and the palette |
| `shell.nav-text` | `gray-300` | Inactive nav item |
| `shell.nav-active-bg` | `gray-910` | Active nav item |
| `shell.nav-active-text` | `gray-50` | Active nav item label |
| `shell.nav-badge-bg` | `accent.primary` | Inbox count badge (digits `on-accent`) |
| `shell.topbar-hover` | `gray-920` | Top-bar button hover (command field, remote status) |
| `shell.topbar-text` | `gray-300` | Command field placeholder, remote status label |
| `shell.status-published` | `accent.primary` | Remote-status dot, nothing to publish (halo `lime-400/15`) |
| `shell.status-pending` | `accent.yours` | Remote-status dot, versions to publish (halo `violet-400/18`) |
| `shell.logo-mark` | `accent.primary` | The X in the product mark |
| `shell.logo-bg` | `gray-990` | Product mark tile |
| `shell.logo-ring` | `gray-860` | 1px ring around the product mark |

#### Buttons
| Token | Primitive | Element |
|---|---|---|
| `button.primary-bg` | `accent.primary` | Primary button |
| `button.primary-hover` | `accent.primary-hover` |  |
| `button.primary-text` | `on-accent` |  |
| `button.secondary-bg` | `gray-880` | Secondary button, Reset button |
| `button.secondary-hover` | `gray-850` |  |
| `button.secondary-text` | `gray-50` |  |
| `button.ghost-text` | `gray-200` | Ghost button label (or `text.muted` in quiet spots) |
| `button.ghost-hover` | `gray-880` | Ghost / icon button hover |
| `button.danger-text` | `accent.danger` | Danger ghost (Remove, Cancel, Restore original) |
| `button.danger-hover` | `red-400/10` |  |
| `source-pill.bg` | `gray-910` | Source pill in Add, inactive (label `text.secondary`) |
| `source-pill.active-bg` | `gray-50` | Source pill in Add, active |
| `source-pill.active-text` | `on-accent` | |
| `button.toast-action-text` | `accent.primary` | Action inside a toast |

#### Status pills
| Token | Primitive | Element |
|---|---|---|
| `pill.update-bg` | `accent.primary` | "Update" |
| `pill.update-text` | `on-accent` |  |
| `pill.neutral-bg` | `gray-860` | "Not published", "Adopt", "Put back" |
| `pill.neutral-hover` | `gray-800` |  |
| `pill.neutral-text` | `gray-200` |  |
| `pill.removed-bg` | `red-400/12` | "Removed from source" |
| `pill.removed-text` | `accent.danger` |  |
| `pill.disabled-bg` | `amber-400/10` | Plugin "Disabled" |
| `pill.disabled-text` | `accent.attention` |  |
| `pill.version-bg` | `gray-880` | Plugin version |
| `pill.version-text` | `gray-300` |  |

#### Inputs, switches, checkboxes
| Token | Primitive | Element |
|---|---|---|
| `input.bg` | `gray-980` | Text inputs, search fields, command field |
| `input.border` | `gray-860` |  |
| `input.border-focus` | `gray-700` |  |
| `input.text` | `gray-50` |  |
| `input.placeholder` | `gray-500` | Placeholder text and leading icon |
| `switch.on-track` | `accent.primary` | Switch on (full and compact) |
| `switch.on-knob` | `on-accent` |  |
| `switch.off-track` | `gray-850` | Switch off |
| `switch.off-knob` | `gray-400` |  |
| `checkbox.ring` | `gray-700` | Unchecked popover checkbox |
| `checkbox.checked-bg` | `accent.primary` |  |
| `checkbox.check` | `on-accent` |  |

#### Segmented control (Update review, filter State)
| Token | Primitive | Element |
|---|---|---|
| `segmented.track-bg` | `gray-950` | Track (`gray-980` when it sits on a card) |
| `segmented.track-border` | `gray-870` |  |
| `segmented.text` | `gray-50` | Unselected segment; `gray-400` once another segment is chosen |
| `segmented.yours-selected` | `accent.yours` | "Keep yours" (label `on-accent`) |
| `segmented.theirs-selected` | `accent.theirs` | "Use original / update" |
| `segmented.both-selected` | `gray-50` | "Keep both" |
| `segmented.neutral-selected` | `gray-850` | Filter State segments (label `text.primary`) |

#### Filter row (Skills toolbar)
| Token | Primitive | Element |
|---|---|---|
| `filter.bg` | `gray-880` | Filter button at rest |
| `filter.text` | `gray-300` |  |
| `filter.active-bg` | `lime-400/12` | Filter button with any filter applied |
| `filter.active-text` | `accent.primary` |  |
| `filter.badge-bg` | `accent.primary` | Count badge (digits `on-accent`) |
| `filter.chip-bg` | `gray-920` | Applied agent chip |
| `filter.chip-text` | `gray-150` |  |
| `filter.chip-hover` | `gray-880` |  |
| `filter.sort-text` | `gray-300` | A–Z toggle |

#### Lists and rows
| Token | Primitive | Element |
|---|---|---|
| `row.hover` | `gray-920` | Skills row hover |
| `row.selected-bg` | `gray-900` | Selected Skills row |
| `row.selected-bar` | `accent.primary` | 2px bar on the selected row |
| `row.name` | `gray-50` | Row name, enabled |
| `row.name-off` | `gray-300` | Row name when in no agent |
| `row.desc` | `gray-300` | Description line |
| `row.chip-ring` | `shell.content-bg` | 2px ring behind overlapping agent logos in rows |
| `group.label` | `gray-300` | Source group caption |
| `group.count` | `gray-600` | Count beside the caption |
| `group.divider-label` | `gray-600` | "NOT IN ANY AGENT" sub-divider |
| `table.row-hover` | `gray-930` | Hover on Inbox update rows, server rows, Settings sources |
| `table.header-text` | `gray-500` | Column captions |

#### Skill glyph
| Token | Primitive | Element |
|---|---|---|
| `glyph.enabled` | `accent.primary` | Managed dot / fork glyph when enabled (and published) |
| `glyph.off` | `gray-600` | Glyph when in no agent or unpublished |
| `glyph.edited` | `accent.attention` | Pencil glyph |

#### Agent logos and toggle chips
| Token | Primitive | Element |
|---|---|---|
| `logo.bg` | `white` | Round agent logo |
| `logo.ring` | = parent bg | 2px ring between overlapping logos (`shell.content-bg`, `overlay.bg`, `card.bg`…) |
| `agent-chip.off-bg` | `gray-950` | Toggle chip, off |
| `agent-chip.off-border` | `gray-870` |  |
| `agent-chip.on-bg` | `lime-400/6` | Toggle chip, on |
| `agent-chip.on-border` | `lime-400/35` |  |
| `agent-chip.hover-border` | `gray-700` |  |
| `agent-chip.text` | `gray-50` | Agent name |

#### Cards and banners
| Token | Primitive | Element |
|---|---|---|
| `card.bg` | `gray-950` | Inbox card, agent card, plugin card, file-tree card, expanded review file card |
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
| `add-card.status-disk` | `accent.attention` | "On disk" label |
| `banner.title` | `gray-50` | Banner title (bg/border/tile from `tone.*`) |
| `banner.text` | `gray-300` |  |
| `plugin.skills-dot` | `accent.yours` | SKILLS group (tint `violet-400/10`) |
| `plugin.servers-dot` | `accent.theirs` | SERVERS group (tint `blue-400/10`) |
| `plugin.hooks-dot` | `accent.attention` | HOOKS group (tint `amber-400/10`) |
| `plugin.connector` | `gray-870` | Nested-content connector lines |

#### Overlays (dialog, popover, palette, sheet, toast, floating bar)
| Token | Primitive | Element |
|---|---|---|
| `overlay.bg` | `gray-940` | Dialog, popover, palette, toast, floating bar |
| `overlay.border` | `gray-800` |  |
| `overlay.shadow` | `0 40px 100px -20px black/80` | Dialog, palette, sheet |
| `popover.shadow` | `0 24px 60px -12px black/70` |  |
| `popover.row-hover` | `gray-880` |  |
| `popover.caption` | `gray-500` | SHOW / STATE / AGENTS |
| `palette.row-text` | `gray-150` |  |
| `palette.row-active` | `gray-870` |  |
| `palette.icon` | `gray-400` |  |
| `palette.icon-active` | `accent.primary` |  |
| `dialog.title` | `gray-50` |  |
| `dialog.subtitle` | `gray-400` |  |
| `sheet.bg` | `gray-960` | History sheet body |
| `sheet.border` | `gray-850` |  |
| `toast.text` | `gray-50` |  |

#### Code, file tree and diffs
| Token | Primitive | Element |
|---|---|---|
| `code.bg` | `gray-980` | Code pane, hunk block |
| `code.border` | `gray-890` | Hunk block border |
| `code.line-number` | `gray-700` |  |
| `code.frontmatter` | `gray-400` | Front-matter and keys |
| `code.heading` | `gray-50` | Markdown headings |
| `code.list` | `gray-150` | List items |
| `code.prose` | `gray-250` | Prose |
| `code.strong` | `gray-100` | Code in diff panes |
| `code.context` | `gray-500` | Context lines around a change |
| `file-row.hover` | `gray-920` | File tree row hover |
| `file-row.divider` | `gray-890` |  |
| `file-row.path` | `gray-200` | Path; `gray-50` when the file is open |
| `file-row.meta` | `gray-500` | "{n} lines" |
| `file-row.edited-label` | `accent.attention` | "edited by you" / "added by you" |
| `diff.added-bg` | `amber-400/8` | Added line tint (marker "+" `accent.attention`) |
| `diff.removed-bg` | `red-400/8` | Removed line tint (marker "−" `accent.danger`) |
| `diff.yours-label` | `accent.yours` | "Yours" label, dot and chosen-pane ring |
| `diff.yours-bg` | `violet-400/6` | "Yours" pane ground |
| `diff.theirs-label` | `accent.theirs` | "Original / Update" label, dot and chosen-pane ring |
| `diff.theirs-bg` | `blue-400/6` | "Original / Update" pane ground |
| `diff.file-pending` | `accent.attention` | File dot while changes remain |
| `diff.file-done` | `accent.primary` | File dot when every change is decided |
| `diff.progress-done` | `accent.primary` | Progress segment, decided |
| `diff.progress-pending` | `gray-850` | Progress segment, pending |

#### Timeline (History)
| Token | Primitive | Element |
|---|---|---|
| `timeline.dot-install` | `gray-600` | Added / found / created entries |
| `timeline.dot-update` | `accent.primary` | Updated, adopted, put back |
| `timeline.dot-yours` | `accent.yours` | Published, forked |
| `timeline.connector` | `gray-890` | 1px line between dots |
| `timeline.meta` | `gray-500` | Relative time under an entry |
| `timeline.more-ring` | `gray-700` | Hollow "Show all" dot ring |

#### Startup checks
| Token | Primitive | Element |
|---|---|---|
| `check.pass-bg` | `lime-400/12` | ✓ mark tile |
| `check.pass-mark` | `accent.primary` |  |
| `check.fail-bg` | `red-400/12` | "!" mark tile |
| `check.fail-mark` | `accent.danger` |  |
| `check.wait-bg` | `gray-880` | Pending mark tile |
| `check.wait-mark` | `gray-500` |  |
| `check.label` | `gray-50` | Check title (`gray-300` while pending) |
| `check.detail` | `gray-400` | Mono detail line |
| `check.fix-bg` | `gray-980` | Fix command chip |
| `check.fix-border` | `gray-870` |  |

#### MCP servers
| Token | Primitive | Element |
|---|---|---|
| `server.transport-bg` | `gray-880` | Transport pill |
| `server.transport-text` | `gray-200` |  |
| `server.command` | `gray-500` | Command / URL under the name |
| `server.tools` | `gray-200` | "{n} tools" |
| `server.tools-auth` | `accent.attention` | "needs sign-in" |
| `server.tools-fail` | `accent.danger` | "unreachable", "timed out", "handshake failed" |
| `server.tools-none` | `gray-600` | "Not checked" |
| `server.versions` | `accent.attention` | "{n} versions" when agents disagree |

### Contrast rules
Body text is `text.primary`–`text.muted` on any shell, card or overlay background. `text.hint`–`text.faint` only for captions, counts, line numbers and placeholders at 12.5px or smaller. Never put alpha-muted text on accent fills.

## 3. Typography

Fonts (bundled with the app from `@fontsource` packages; the app never loads anything from the network): **Instrument Serif** 400 (titles), **Geist** 400–700 (UI), **Geist Mono** 400–600 (technical).

Each row is a type token: a role with its face, size, weight, line height, tracking and case. A token with one value per column is a `type-*` utility in `tokens.css`; a row with a range (`11–12`) is guidance, not a utility – add a token with one value when a component needs it. Text colour is a separate colour token (§2.2). An empty cell inherits.

| Token | Face | Size | Weight | Line height | Tracking | Case | Element |
|---|---|---|---|---|---|---|---|
| `type.page-title` | serif | 56 | 400 | 1 | -0.02em | | Page title |
| `type.inbox-headline` | serif | 68 | 400 | | | | Inbox headline; second clause italic `text.muted` |
| `type.review-title` | serif | 52 | 400 | | | | Review title |
| `type.dialog-title` | serif | 36 | 400 | | | | Dialog and sheet title |
| `type.startup-title` | serif | 48 | 400 | | | | Startup title |
| `type.skill-name` | sans | 30 | 600 | | -0.035em | | Detail pane skill name |
| `type.body` | sans | 14 | 400 | | | | Body, palette rows |
| `type.row-name` | sans | 14 | 500 | | | | Row name |
| `type.row-desc` | sans | 12.5 | 400 | | | | Row description (`text.muted`, single line, ellipsis) |
| `type.secondary` | sans | 13 | 400 | | | | Secondary line (`text.hint`) |
| `type.button` | sans | 14 | | | | | Buttons; the variant sets the weight (600 primary, 500 others) |
| `type.button-sm` | sans | 13.5 | | | | | Small buttons |
| `type.pill` | sans | 11–12 | 600 | | | | Pills |
| `type.caption` | mono | 10.5 | 400 | | 0.16em | upper | Captions (`text.caption`) |
| `type.technical` | mono | 12–13 | 400 | | | | Paths, ids, dates, commands |
| `type.code` | mono | 12.5 | 400 | 1.75 | | | Code pane |
| `type.brand` | sans | 14 | 600 | | -0.01em | | Product name in the sidebar |
| `type.nav` | sans | 14 | 500 | | | | Nav item |
| `type.command-field` | sans | 13 | 400 | | | | Command field placeholder |
| `type.key-hint` | mono | 11 | 400 | | | | Key hint (`esc`, `⌘K`) |
| `type.palette-input` | sans | 17 | 400 | | | | Palette search input |
| `type.palette-hint` | mono | 12 | 400 | | | | Hint at the end of a palette row, keys in the palette footer |
| `type.palette-footer` | sans | 12 | 400 | | | | Palette footer (`text.hint`) |
| `type.toast` | sans | 13.5 | 400 | | | | Toast message |
| `type.toast-action` | sans | 13 | 600 | | | | Action inside a toast |

Text wrapping: `text-wrap:pretty` on multi-line copy. Single-line cells use `white-space:nowrap; overflow:hidden; text-overflow:ellipsis`.

## 4. Layout and spacing

Spacing is a semantic token per element, not a shared scale. Two tokens with the same step name (`button.gap`, `card.gap`) are independent values; change one without touching the other. Primitive px values are listed so the system can be audited, but code references the token.

A size or gap inside a component that no token lists uses Tailwind's spacing steps in whole or half steps, a 2px grid (`gap-2.5` is 10px, `pb-5.5` is 22px). Never a quarter step or an arbitrary value; a value off the grid needs a token.

### Window and shell
| Token | px | Element |
|---|---|---|
| `shell.titlebar-height` | 50 | Title bar |
| `shell.sidebar-width` | 216 | Sidebar |
| `shell.sidebar-padding` | 6 12 14 14 | Sidebar inner padding |
| `shell.nav-height` | 36 | Nav item |
| `shell.nav-gap` | 2 | Between nav items |
| `shell.nav-icon-gap` | 11 | Icon → label in a nav item |
| `shell.command-width` | 460 | Command field |
| `shell.command-height` | 32 | |
| `shell.status-height` | 30 | Remote status pill |
| `shell.panel-inset` | 10 | Main panel inset from the right and bottom |

### Pages
| Token | px | Element |
|---|---|---|
| `page.padding` | 40 48 48 | Content pages (Inbox, Agents, Servers, Plugins, Settings) |
| `page.title-gap` | 28 | Title block → first section |
| `page.section-gap` | 28–40 | Between sections |
| `page.max-width` | 1200 | Inbox |
| `split.list-width` | 408 | Skills left column |
| `split.header-padding` | 30 20 0 28 | Skills list header |
| `split.toolbar-padding` | 20 20 12 28 | Search + filter row block |
| `split.toolbar-gap` | 12 | Search → filter row |
| `split.filter-gap` | 6 | Filter button → chip → sort |
| `split.list-padding` | 0 12 24 14 | Scrolling list |
| `detail.padding` | 34 40 48 | Detail pane content |
| `detail.max-width` | 900 | |
| `detail.section-gap` | 28 | Header → banner → agents → files → details/history |
| `detail.header-gap` | 12 | Name row → description → actions |
| `detail.columns-gap` | 40 | Details ↔ History |
| `detail.kv-gap` | 12 12 | Details key/value grid (row, column) |
| `detail.kv-key-width` | 96 | |

### Lists and rows
| Token | px | Element |
|---|---|---|
| `row.padding` | 11 12 11 14 | Split list row |
| `row.gap` | 12 | Glyph → text |
| `row.line-gap` | 3 | Name row → description |
| `row.inline-gap` | 8 | Name → pills → chips |
| `row.spacing` | 2 | Between rows |
| `row.glyph-col` | 18 | Glyph column |
| `group.padding-first` | 4 14 8 | First group header |
| `group.padding` | 20 14 8 | Later group headers |
| `group.gap` | 8 | Label → count |
| `table.header-height` | 40 | Sticky header (servers) |
| `table.row-padding` | 16 | Server rows |
| `table.col-gap` | 24 | |

### Buttons, pills, controls
| Token | px | Element |
|---|---|---|
| `button.primary-height` | 38–40 | |
| `button.primary-padding-x` | 20 | |
| `button.secondary-height` | 34–38 | 34 in the detail pane, 36–38 elsewhere |
| `button.secondary-padding-x` | 14–16 | |
| `button.ghost-height` | 28–36 | |
| `button.ghost-padding-x` | 12–16 | |
| `button.icon-size` | 24–38 | Square icon buttons |
| `button.gap` | 8 | Between sibling buttons (6 in the detail action row) |
| `button.icon-gap` | 7–8 | Icon → label inside a button |
| `pill.height` | 18–22 | 20 in list rows, 22 in the detail header |
| `pill.padding-x` | 7–10 | |
| `switch.size` | 36 22 | Track w h |
| `switch.padding` | 3 | |
| `switch.knob` | 16 | |
| `switch.compact-size` | 30 18 | Inside agent toggle chips |
| `switch.compact-knob` | 14 | |
| `checkbox.size` | 16 | |
| `segmented.padding` | 2–3 | Track padding |
| `segmented.gap` | 2 | |
| `segmented.height` | 24–28 | Segment |
| `segmented.padding-x` | 10–12 | |
| `filter.height` | 30 | Filter button and agent chip |
| `filter.badge` | 16 | Count badge |
| `agent-chip.height` | 44 | Agent toggle chip |
| `agent-chip.padding` | 0 8 0 7 | |
| `agent-chip.gap` | 10 | Logo → name → switch |
| `agent-chip.wrap-gap` | 8 | Between chips |
| `logo.overlap` | −6 to −8 | Negative margin between stacked logos |
| `logo.ring` | 2 | Ring width |
| `logo.padding` | 3–5 | Inner padding by size (3 at 20px, 4 at 24–26, 5 at 28) |

### Inputs
| Token | px | Element |
|---|---|---|
| `input.height` | 42 | Dialog and settings inputs |
| `input.padding-x` | 14 | |
| `input.search-height` | 36 | List search field |
| `input.search-padding` | 0 14 0 34 | Room for the icon |
| `input.label-gap` | 8 | Label → field |
| `input.textarea-height` | 88 | Create dialog description |

### Cards, banners, tiles
| Token | px | Element |
|---|---|---|
| `card.padding` | 16–18 | Inbox card |
| `card.gap` | 12 | Between Inbox cards |
| `card.header-gap` | 14 | Icon tile → text → actions |
| `card.tile` | 34 | Icon tile |
| `card.row-padding` | 10 0 | Sub-rows inside the Updates card |
| `banner.padding` | 14 16 14 14 | |
| `banner.gap` | 14 | Tile → text → actions |
| `banner.tile` | 32–34 | |
| `banner.text-gap` | 3 | Title → text |
| `agent-card.padding` | 22 | Agents page cards |
| `agent-card.gap` | 12 | Grid gap |
| `agent-card.logo` | 44 | |
| `plugin-card.padding` | 22 | |
| `plugin-card.row-height` | 36 | Tree rows |
| `add-card.gap` | 12 | Grid gap |
| `add-card.min-height` | 120 | |

### Overlays
| Token | px | Element |
|---|---|---|
| `dialog.width` | 480 | |
| `dialog.padding` | 32 | |
| `dialog.gap` | 22 | Between dialog blocks |
| `popover.width` | 280–300 | |
| `popover.padding` | 8–14 | 8 for row lists, 14 for grouped content |
| `popover.gap` | 16 | Between groups |
| `popover.row-height` | 32–34 | |
| `popover.offset` | 8 | Gap from its anchor |
| `sheet.width` | 420 | History sheet |
| `sheet.inset` | 10 | From top/right/bottom |
| `sheet.padding` | 28 32 | |
| `palette.width` | 640 | |
| `palette.input-height` | 60 | |
| `key-hint.padding` | 3 8 | Key hint (`esc`, `⌘K`) |
| `toast.height` | 44 | |
| `toast.padding` | 0 18 | 0 6 0 18 with an action |
| `toast.offset` | 24 | From the bottom edge |
| `floating-bar.offset` | 24 | From the bottom edge |
| `floating-bar.height` | 56 | |

### Code, diff, timeline, startup
| Token | px | Element |
|---|---|---|
| `code.padding-y` | 14 | Pane top/bottom |
| `code.gutter` | 48 12 | Line-number and marker columns |
| `code.gap` | 10–14 | Between gutter and text |
| `code.max-height` | 340–380 | |
| `file-row.height` | 44–46 | File tree row |
| `file-row.padding-x` | 16 | |
| `hunk.gap` | 12 | Between hunk blocks |
| `hunk.header-padding` | 8 8 8 16 | |
| `hunk.pane-margin` | 4 8 | Around the two panes |
| `diff.segment` | 56 4 | Progress segment w h |
| `diff.segment-gap` | 4 | |
| `timeline.col` | 12 | Dot column |
| `timeline.gap` | 12–16 | Dot → text |
| `timeline.dot` | 7 | |
| `timeline.entry-gap` | 16–20 | Bottom padding per entry |
| `check.column-width` | 480 | Startup column |
| `check.block-gap` | 36 | Logo/title → checks → actions |
| `check.row-gap` | 22 | Between checks |
| `check.mark` | 20 | Status circle |
| `check.mark-gap` | 14 | Mark → text |

## 5. Radii

Radii are scoped the same way. Same-named steps across elements are independent.

| Token | px | Element |
|---|---|---|
| `button.radius` | 9999 | All buttons |
| `button.icon-radius` | 6 or 50% | Small icon buttons |
| `pill.radius` | 9999 | Status pills, filter chip, source pills, version pill |
| `switch.radius` | 9999 | Track and knob |
| `checkbox.radius` | 50% | Popover checkbox |
| `segmented.radius` | 9999 | Track and segments |
| `agent-chip.radius` | 9999 | Agent toggle chip |
| `logo.radius` | 50% | Agent logo |
| `shell.nav-radius` | 10 | Nav item |
| `shell.logo-radius` | 8 | Sidebar product mark |
| `shell.status-radius` | 9999 | Remote status |
| `shell.command-radius` | 9999 | Command field |
| `shell.panel-radius` | 14 | Main panel |
| `input.radius` | 12 | Inputs, textareas |
| `input.search-radius` | 9999 | Search field |
| `row.radius` | 12 | Split list rows |
| `table.row-radius` | 14 | Server rows |
| `popover.row-radius` | 9–10 | Popover rows, Reset button |
| `card.radius` | 18 | Inbox cards, review file cards |
| `card.tile-radius` | 10 | Icon tile |
| `file-card.radius` | 16 | File tree card |
| `banner.radius` | 18 | |
| `agent-card.radius` | 20 | Agents page cards |
| `plugin-card.radius` | 20 | |
| `add-card.radius` | 18 | |
| `hunk.radius` | 14 | Hunk block |
| `hunk.pane-radius` | 10 | Yours / theirs panes (outer corners only) |
| `diff.segment-radius` | 4 | Progress segment |
| `popover.radius` | 16–18 | |
| `dialog.radius` | 24 | |
| `sheet.radius` | 20 | |
| `palette.radius` | 20 | |
| `palette.row-radius` | 12 | Palette rows |
| `key-hint.radius` | 9999 | Key hint |
| `toast.radius` | 9999 | |
| `floating-bar.radius` | 9999 | |
| `check.mark-radius` | 50% | Startup status circle |
| `check.fix-radius` | 8 | Fix command chip |
| `startup.logo-radius` | 12 | Product mark on the startup layer |

## 6. Components

### Buttons (height / padding / colours)
- **Primary** – `button.primary-height` / 0 `button.primary-padding-x` / `button.primary-bg`, `button.primary-text`, 600; hover `button.primary-hover`. Disabled: `opacity:.3` and inert (never a grey fill).
- **Secondary** – `button.secondary-height` / 0 `button.secondary-padding-x` / `button.secondary-bg`, `button.secondary-text`, 500; hover `button.secondary-hover`.
- **Ghost** – `button.ghost-height` / 0 `button.ghost-padding-x` / transparent, `button.ghost-text` (or `text.muted`); hover `button.ghost-hover`, `text.primary`.
- **Danger** – ghost in `button.danger-text`; hover `button.danger-hover`.
- **Icon** – `button.icon-size` square, `button.radius` or `button.icon-radius`, transparent; hover `button.ghost-hover`.
- **Toast action** – ghost with `button.toast-action-text` on `overlay.bg`.

Two buttons side by side: `button.gap`, primary on the right.

### Pills (status)
Height `pill.height`, padding 0 `pill.padding-x`, `pill.radius`, 11–12 / 600.
- Update: `pill.update-bg` / `pill.update-text` (clickable).
- Not published / Adopt: `pill.neutral-bg` / `pill.neutral-text`.
- Removed from source: `pill.removed-bg` / `pill.removed-text`.
- Disabled (plugin): `pill.disabled-bg` / `pill.disabled-text`.
- Version (plugin): `pill.version-bg` / `pill.version-text`, mono 12.

### Switch
`switch.size`, `switch.padding`, `switch.radius`; knob `switch.knob` round. On: track `switch.on-track`, knob `switch.on-knob`, `justify-content:flex-end`. Off: track `switch.off-track`, knob `switch.off-knob`. `transition: background .18s`. The whole row is the hit target. Compact variant `switch.compact-size` (knob `switch.compact-knob`) inside agent toggle chips.

### Agent toggle chip (detail pane AGENTS)
Pill button `agent-chip.height`, `agent-chip.padding`, `agent-chip.gap`, `agent-chip.radius`: 28px logo(s) · name 13 / 500 · compact switch. Off: `agent-chip.off-bg` with 1px `agent-chip.off-border`. On: `agent-chip.on-bg` with `agent-chip.on-border`. Hover border `agent-chip.hover-border`. Locked: 45% opacity, `cursor:not-allowed`, `title` holds the reason; a click toasts it. Chips wrap in a row with `agent-chip.wrap-gap`.

### Checkbox (popover)
16×16 round, inset ring 1.5px `checkbox.ring`; checked: `checkbox.checked-bg` fill, `checkbox.check` check.

### Segmented control
Track `segmented.track-bg`, `segmented.track-border`, `segmented.radius`, `segmented.padding`, `segmented.gap`. Segments `segmented.height` high, `segmented.radius`, 12–12.5 / 500. Selected segment fills with its meaning colour (`segmented.yours-selected`, `segmented.theirs-selected`, `segmented.both-selected`, `segmented.neutral-selected`) and `on-accent`; others transparent `segmented.text`.

### Agent logo chip
Round, `logo.bg`, `padding:3–4px`, `background-size:contain`, logo from an image bundled with the app (never fetched from the network). Sizes: 16 (filter chip), 20 (list rows, popover), 24 (server rows), 28 (agent toggle chips), 44 (agent cards). Overlap with `logo.overlap` and a `logo.ring`-wide ring (`box-shadow: 0 0 0 2px <parent bg>`). Every logo carries `title="<agent name>"`.

### Skill glyph (16px, leading the row)
- dot (filled) – managed; `glyph.enabled` when enabled, `glyph.off` when not.
- pencil – edited locally; `glyph.edited`.
- fork – one of your own skills; `glyph.enabled` when enabled and published, `glyph.off` otherwise.
`title` explains the state ("Fork · Enabled", "Edited locally · Not in any agent").

### Group header (lists)
Mono caption 10.5–11 / `.16em` uppercase `group.label` plus a count in `group.count`. `group.padding-first` for the first group, `group.padding` for the rest.

### Banner (detail pane)
`banner.radius`, `banner.padding`, `tone.*.bg` + 1px `tone.*.border`; `banner.tile` icon tile in `tone.*.fg`; title 14 / 500 `banner.title`; text 13 `banner.text`; actions on the right.

### Card (Inbox)
`card.bg`, `card.radius`, `card.border`, `card.padding`. Header grid: `card.tile` icon tile (`tone.*`) · title/tag/text · actions. Resolve animation: `opacity 0; transform: translateX(24px) scale(.985)` over 280ms, then removed.

### Toast
`toast.offset` from the bottom, centred; `overlay.bg`, `overlay.border`, `toast.radius`, `toast.height`, `toast.padding`; text 13.5 `toast.text`; optional action button ghost in `button.toast-action-text`. Auto-dismiss 2.6s, or 5s when it has an action. One toast at a time.

### Timeline (history)
Grid `timeline.col 1fr`, `timeline.gap`. Dot `timeline.dot` round (`timeline.dot-install`, `timeline.dot-update`, `timeline.dot-yours` by entry kind), a 1px `timeline.connector` below it. Entry: message 14 `text.primary`, meta 12 `timeline.meta`. Final item when truncated: hollow dot (inset ring 1.5px `timeline.more-ring`) and a ghost button "Show all {n}".

### Code pane
`code.bg`, mono 12.5, `line-height:1.75`, grid `code.gutter 1fr` (line no. `code.line-number` right-aligned · marker · text). Front-matter and keys `code.frontmatter`, headings `code.heading`, list items `code.list`, prose `code.prose`. Diff tints: added line `+` on `diff.added-bg`, removed line `−` on `diff.removed-bg` with no number.

### Key hint
`esc` in the palette and `⌘K` / `Ctrl K` in the command field (`src/components/key-hint.tsx`): `key-hint.padding`, `key-hint.radius`, `button.secondary-bg` fill, `type.key-hint` in `text.muted`.

### Inputs
`input.height`, `input.bg`, `input.border`, `input.radius`, padding 0 `input.padding-x`, 14px; focus `input.border-focus`. Mono for URLs and commands. Search field in lists: `input.search-height`, `input.search-radius`, `input.search-padding`, icon at left, placeholder `input.placeholder`.

## 7. Iconography

Stroke icons on a 24 grid, `stroke-width` 1.75–2.25, round caps/joins, 13–16px rendered (18 for the palette's search icon). Fill only for the managed "dot" glyph. No icon fonts, no emoji. The product mark is `shell.logo-mark` on `shell.logo-bg` (`src/components/agentx-mark.tsx`), 26px `shell.logo-radius` in the sidebar, 40px `startup.logo-radius` on the startup layer, with a 1px `shell.logo-ring` ring.

## 8. Motion

- Hover/background changes: no transition or ≤ .18s.
- Switches: `.18s` background.
- Chevrons: `rotate(90deg)` with `.15s`.
- Floating bar: `translateY(24px) → 0`, opacity 0 → 1, `.22s ease`.
- Card resolve: 280ms, see §6.
- Spinner-style icons (Check updates, Check tools): one full rotation `.9–1s ease`, then a toast.
- Diff pane choice: `opacity .2s, box-shadow .2s`.
- Primary button enable/disable: `opacity .2s`.
Nothing loops forever.

## 9. Copy rules

- Plain verbs: Add, Remove, Update, Fork, Publish, Review update, Put back, Adopt, Check updates.
- Never: sync, commit, push, hunk, merge, upstream, remote, repo (say "source", "original", "your other machines", "change").
- "Your skills" for the user's own skills (forked or created); "the original" for the skill a fork came from; "the update" for a new version from a skill's source. Fork is an action, not a kind of skill (`GLOSSARY.md`).
- Dates over ids in rows ("Sep 14 → Sep 28"); ids with dates in Details ("3f2a9c1 · Sep 14").
- Toasts state what happened in past tense ("Commit updated", "Put pdf back in Cursor"), and name the undo when one exists.
- Captions are uppercase mono; everything else sentence case. No trailing periods in titles, pills or buttons.
