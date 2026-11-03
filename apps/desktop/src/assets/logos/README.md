# Agent client logos

The marks the `AgentLogo` component (`src/components/agent-logo.tsx`) shows on the round white tile, one per agent client slug. Each is the client's official mark as its vendor publishes it, never a mark drawn for this app (DESIGN.md, section 6 "Agent client logo chip"). A client without a file here gets a monogram.

Every file has a `viewBox`, no script, no external reference and no raster image, and is run through svgo (`removeDimensions`, `removeTitle`, ids kept). Brand colours stay where the vendor's mark has them; a mark the vendor publishes in black only stays black.

| File | Client | Mark and source | Licence or brand note |
| --- | --- | --- | --- |
| `claude-code.svg` | Claude Code | Anthropic's Claude mark in its brand colour `#D97757`, taken from `@lobehub/icons-static-svg` 1.102.0 (`icons/claude-color.svg`), which redistributes the vendor's SVG. | The package is MIT licensed (github.com/lobehub/lobe-icons). The mark is Anthropic's trademark, used to name the client. |
| `codex.svg` | Codex | OpenAI's Codex product icon with its gradient colours, taken from `@lobehub/icons-static-svg` 1.102.0 (`icons/codex-color.svg`). The gradient id is renamed to `codex-g0`. | Package MIT licensed. The mark is OpenAI's trademark, used to name the client. |
| `cursor.svg` | Cursor | Cursor's cube, the 2D mark for light backgrounds (`General Logos/Cube/SVG/CUBE_2D_LIGHT.svg`, fill `#26251e`) from the brand asset pack on cursor.com/brand. The stylesheet class is inlined as a `fill` attribute. | Published by Cursor for brand use on cursor.com/brand. The mark is Anysphere's trademark. |
| `gemini-cli.svg` | Gemini CLI | Google's Gemini mark with its gradient colours, taken from `@lobehub/icons-static-svg` 1.102.0 (`icons/gemini-color.svg`). Gradient ids renamed to `gemini-g0` to `gemini-g2`. | Package MIT licensed. The mark is Google's trademark, used to name the client. |
| `github-copilot.svg` | GitHub Copilot | GitHub's Copilot mark from GitHub's own icon set, `@primer/octicons` 19.42.0 (`build/svg/copilot-24.svg`), black as published. | Octicons are MIT licensed (github.com/primer/octicons). The mark is GitHub's trademark. |
| `windsurf.svg` | Windsurf | The Windsurf symbol, black (`#0B100F`), from windsurf.com/brand (`windsurf-black-symbol.svg`). The `viewBox` is cropped from the 1024 canvas to the symbol's bounds; the path is unchanged. | Published by Windsurf for brand use on windsurf.com/brand, which asks for no outline, effects, rotation, gradient or recolouring. The mark is Windsurf's trademark. |

To add a client: take the vendor's published SVG, keep its name as `<client slug>.svg`, optimise it with `pnpm dlx svgo`, list it here and add it to `agent-logo.tsx`.
