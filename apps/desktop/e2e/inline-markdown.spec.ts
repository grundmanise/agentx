// parseInlineMarkdown and markdownText, as a table: a skill's description as its SKILL.md holds
// it, and the pieces the app renders.
import { expect, test } from "@playwright/test";

import { markdownText, parseInlineMarkdown } from "#/lib/inline-markdown.ts";
import type { InlineNode } from "#/lib/inline-markdown.ts";

const code = (text: string): InlineNode => ({ kind: "code", text });
const strong = (...children: InlineNode[]): InlineNode => ({
  children,
  kind: "strong",
});
const em = (...children: InlineNode[]): InlineNode => ({
  children,
  kind: "em",
});

const table: [string, InlineNode[]][] = [
  ["Plain text", ["Plain text"]],
  ["Run `git status` first", ["Run ", code("git status"), " first"]],
  ["``a ` tick``", [code("a ` tick")]],
  ["` padded `", [code("padded")]],
  ["An `open code span", ["An `open code span"]],
  ["Use **always** here", ["Use ", strong("always"), " here"]],
  ["Use __always__ here", ["Use ", strong("always"), " here"]],
  ["*one* and _two_", [em("one"), " and ", em("two")]],
  ["~~old~~ new", [{ children: ["old"], kind: "del" }, " new"]],
  ["**Bold with `code`**", [strong("Bold with ", code("code"))]],
  ["*a **b** c*", [em("a ", strong("b"), " c")]],
  ["Keeps snake_case_name", ["Keeps snake_case_name"]],
  ["2 * 3 * 4", ["2 * 3 * 4"]],
  ["A `*not em*` span", ["A ", code("*not em*"), " span"]],
  ["\\*literal\\*", ["*literal*"]],
  ["See [the **docs**](https://x.dev)", ["See the ", strong("docs")]],
  ["Go to <https://x.dev>", ["Go to https://x.dev"]],
  ["A <tag> stays", ["A <tag> stays"]],
  ["**unclosed", ["**unclosed"]],
  ["# Not a heading", ["# Not a heading"]],
  ["R&amp;D", ["R&D"]],
  ["R&D &#65;", ["R&D A"]],
  ["`a &amp; b`", [code("a &amp; b")]],
  ["See ![the logo](logo.png) here", ["See the logo here"]],
  ["Docs at https://x.dev now", ["Docs at https://x.dev now"]],
];

for (const [text, nodes] of table) {
  test(`parseInlineMarkdown: ${text}`, () => {
    expect(parseInlineMarkdown(text)).toEqual(nodes);
  });
}

test("markdownText drops the Markdown", () => {
  expect(
    markdownText("Run `git status` **before** a [commit](https://x.dev)")
  ).toBe("Run git status before a commit");
});
