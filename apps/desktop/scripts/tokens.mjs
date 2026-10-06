// Generates src/styles/tokens.css from the token tables in DESIGN.md.
//
//   node scripts/tokens.mjs          write the file
//   node scripts/tokens.mjs --check  exit 1 when the file is out of date
//
// See the top of DESIGN.md for how token names map to CSS variables.
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
export const designPath = join(root, "DESIGN.md");
export const tokensPath = join(root, "src/styles/tokens.css");

// Returns the rows of every table, each tagged with the part of DESIGN.md it is in:
// primitives (2.1), colors (2.2), layout (4) or radii (5).
function tables(markdown) {
  const out = [];
  let part = "";
  for (const line of markdown.split("\n")) {
    const top = line.match(/^## (\d+)\./)?.[1];
    if (top) part = { 2: "colors", 4: "layout", 5: "radii" }[top] ?? "";
    if (line.startsWith("### 2.1")) part = "primitives";
    if (line.startsWith("### 2.2")) part = "colors";
    if (!line.startsWith("|") || /^\|[-| ]+\|$/.test(line)) continue;
    out.push({ part, cells: line.slice(1, -1).split("|").map((c) => c.trim()) });
  }
  return out;
}

const code = (cell) => cell.match(/^`([^`]+)`/)?.[1];
const varName = (token) => token.replace(/\./g, "-");

function hexToRgb(hex) {
  const n = parseInt(hex.slice(1), 16);
  return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

export function generate(markdown) {
  const rows = tables(markdown);
  const primitives = new Map();
  const colors = [];
  const shadows = [];
  const radii = [];
  const layout = [];
  const semantic = new Set();

  for (const { part, cells } of rows) {
    const name = code(cells[0]);
    if (!name) continue;

    if (part === "primitives") {
      const hex = code(cells[1]);
      if (hex) primitives.set(name, hex);
    } else if (part === "colors") {
      if (name.startsWith("tone.")) {
        for (const [i, key] of ["fg", "bg", "border"].entries()) colors.push([`${name}-${key}`, code(cells[i + 1])]);
      } else if (name.endsWith(".shadow") || name.endsWith("-shadow")) {
        shadows.push([name, code(cells[1])]);
      } else {
        colors.push([name, code(cells[1]) ?? cells[1]]);
      }
    } else if (part === "layout" || part === "radii") {
      const value = cells[1];
      if (!/^-?\d+(\.\d+)?%?( -?\d+(\.\d+)?%?)*$/.test(value)) continue; // ranges and alternatives are guidance
      const css = value.split(" ").map((v) => (v.endsWith("%") || v === "0" ? v : `${v}px`)).join(" ");
      if (part === "radii") radii.push([name.replace(/[.-]radius$/, ""), css]);
      else layout.push([name, css]);
    }
  }
  for (const [name] of colors) semantic.add(name);

  // Resolves a table value to CSS: a primitive, a primitive at an alpha, or another semantic token.
  const resolve = (value) =>
    value.replace(/([a-z]+(?:-\d+)?)\/(\d+)|[a-z][a-z0-9-]*(?:\.[a-z0-9-]+)?/g, (match, prim, alpha) => {
      if (prim) {
        const hex = primitives.get(prim);
        if (!hex) throw new Error(`unknown primitive ${prim}`);
        return `rgb(${hexToRgb(hex).join(" ")} / ${Number(alpha) / 100})`;
      }
      if (primitives.has(match)) return `var(--${match})`;
      if (semantic.has(match)) return `var(--color-${varName(match)})`;
      if (/^\d|px$/.test(match)) return match;
      throw new Error(`unknown token ${match} in "${value}"`);
    });

  const lines = [
    "/* Generated from DESIGN.md by scripts/tokens.mjs. Do not edit; run `pnpm tokens`. */",
    "",
    "/* Primitives: the raw palette. Never referenced from components. */",
    ":root {",
    ...[...primitives].map(([n, hex]) => `  --${n}: ${hex};`),
    "}",
    "",
    "/* Semantic tokens: the only colours, shadows and radii components use. */",
    "@theme static {",
    "  --color-*: initial;",
    "  --shadow-*: initial;",
    "  --radius-*: initial;",
  ];
  for (const [name, value] of colors) {
    if (!value || value.startsWith("=")) continue; // "= parent bg": set where it is used
    lines.push(`  --color-${varName(name)}: ${resolve(value)};`);
  }
  for (const [name, value] of shadows) lines.push(`  --shadow-${varName(name.replace(/[.-]shadow$/, ""))}: ${resolve(value)};`);
  for (const [name, value] of radii) lines.push(`  --radius-${varName(name)}: ${value};`);
  lines.push("}", "", "/* Layout tokens with a single value. */", ":root {");
  for (const [name, value] of layout) lines.push(`  --${varName(name)}: ${value};`);
  lines.push("}", "");
  return lines.join("\n");
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const css = generate(readFileSync(designPath, "utf8"));
  if (process.argv.includes("--check")) {
    if (readFileSync(tokensPath, "utf8") !== css) {
      console.error("src/styles/tokens.css is out of date, run `pnpm tokens`");
      process.exit(1);
    }
  } else {
    writeFileSync(tokensPath, css);
  }
}
