import { readFileSync } from "node:fs";
// @ts-expect-error: plain ESM script without types
import { designPath, generate, tokensPath } from "../../scripts/tokens.mjs";

describe("design tokens", () => {
  it("tokens.css matches DESIGN.md (run `pnpm tokens` after editing DESIGN.md)", () => {
    expect(readFileSync(tokensPath, "utf8")).toBe(generate(readFileSync(designPath, "utf8")));
  });

  it("components use no primitive and no default Tailwind colour", () => {
    const sources = import.meta.glob("../**/*.tsx", { query: "?raw", import: "default", eager: true });
    for (const [file, source] of Object.entries(sources)) {
      expect(source as string, file).not.toMatch(/--(gray|lime|amber|red|violet|blue)-\d|#[0-9a-f]{6}\b/i);
      expect(source as string, file).not.toMatch(/\b(bg|text|border|fill)-(gray|neutral|zinc|slate|white|black|red|lime)\b/);
    }
  });
});
