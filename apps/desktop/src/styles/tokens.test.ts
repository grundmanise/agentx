import { readFileSync } from "node:fs";
// @ts-expect-error: plain ESM script without types
import { designPath, generate, tokensPath } from "../../scripts/tokens.mjs";

describe("design tokens", () => {
  it("tokens.css matches DESIGN.md (run `pnpm tokens` after editing DESIGN.md)", () => {
    expect(readFileSync(tokensPath, "utf8")).toBe(generate(readFileSync(designPath, "utf8")));
  });

  // Belt and braces with the lint (src/styles/lint.test.ts): no source names a raw colour, a
  // primitive or a default Tailwind colour, even in a string the lint does not read as a class.
  it("components use no raw colour, no primitive and no default Tailwind colour", () => {
    const sources = import.meta.glob(["../**/*.{ts,tsx}", "!../**/*.test.{ts,tsx}"], {
      query: "?raw",
      import: "default",
      eager: true,
    });
    const palette =
      "slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose";
    const utility = "bg|text|border|fill|stroke|ring|outline|divide|from|via|to|decoration|accent|caret|shadow|placeholder";
    const rules: [string, RegExp][] = [
      ["hex colour", /#(?:[0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})\b/i],
      ["primitive", new RegExp(`--(?:(?:${palette})-\\d|white\\b|black\\b)`)],
      ["default Tailwind colour", new RegExp(`\\b(?:${utility})-(?:(?:${palette})-\\d{2,3}|white|black)\\b`)],
    ];
    expect(Object.keys(sources).length).toBeGreaterThan(0);
    for (const [file, source] of Object.entries(sources)) {
      for (const [what, pattern] of rules) expect(source as string, `${what} in ${file}`).not.toMatch(pattern);
    }
  });
});
