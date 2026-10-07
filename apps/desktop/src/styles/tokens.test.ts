import { readFileSync } from "node:fs";
import path from "node:path";

// tokens.css is edited by hand, so these checks catch the mistakes a generator used to rule out.
const tokens = readFileSync(path.join(import.meta.dirname, "tokens.css"), "utf-8").replaceAll(/\/\*[^]*?\*\//gu, "");
const declared = new Set([...tokens.matchAll(/^\s*(?<name>--[\w-]+):/gmu)].map((m) => m[1]));
const theme = (/@theme static \{(?<body>[^]*?)\n\}/u.exec(tokens))?.[1] ?? "";

describe("design tokens", () => {
  it("resets Tailwind's whole default theme before declaring ours", () => {
    expect(theme.trim().startsWith("--*: initial;")).toBe(true);
    expect(tokens.match(/:\s*initial;/gu)).toHaveLength(1);
  });

  it("keeps primitives and font faces out of the theme", () => {
    expect(theme).not.toMatch(/^\s*--(?:(?:gray|lime|amber|red|violet|blue)-\d+|white|black|font-(?!weight-)[\w-]+):/mu);
  });

  it("refers only to declared variables", () => {
    const used = [...tokens.matchAll(/var\((?<name>--[\w-]+)/gu)].map((m) => m[1]);
    expect(used.filter((name) => !declared.has(name))).toEqual([]);
  });

  it("declares each variable once", () => {
    const names = [...tokens.matchAll(/^\s*(?<name>--[\w-]+):/gmu)].map((m) => m[1]);
    expect(names.filter((name, i) => names.indexOf(name) !== i)).toEqual([]);
  });

  // The lint checks classes; this also covers strings it does not read as classes, such as an
  // SVG attribute or a custom property set from code.
  it("components use no raw colour and no primitive", () => {
    const sources = import.meta.glob(["../**/*.{ts,tsx}", "!../**/*.test.{ts,tsx}"], {
      eager: true,
      import: "default",
      query: "?raw",
    });
    const primitives = [...declared].filter((name) => /^--(?:gray|lime|amber|red|violet|blue)-\d|^--(?:white|black)$/u.test(name));
    expect(Object.keys(sources).length).toBeGreaterThan(0);
    expect(primitives.length).toBeGreaterThan(0);
    for (const [file, source] of Object.entries(sources)) {
      const text = source;
      expect(text, `hex colour in ${file}`).not.toMatch(/#(?:[0-9a-f]{3,4}|[0-9a-f]{6}|[0-9a-f]{8})\b/iu);
      expect(text, `rgb() or hsl() colour in ${file}`).not.toMatch(/\b(?:rgba?|hsla?|oklch|oklab)\(/iu);
      for (const name of primitives) {expect(text, `primitive ${name} in ${file}`).not.toMatch(new RegExp(`${name}(?![\\w-])`, "u"));}
    }
  });
});
