// The inline Markdown of a one-line text, such as a skill's description from its SKILL.md
// frontmatter, read with marked's inline lexer (GitHub Flavored Markdown): code spans, strong,
// emphasis and strikethrough. A link shows its text and an image its alt text, as a description
// sits inside a row that opens on a click; HTML shows as written. Block syntax (headings, lists,
// quotes) stays as written, as only the inline lexer reads the text. marked keeps a text token
// HTML-escaped for its HTML output, so its entities are decoded: `R&amp;D` reads as R&D.
import { decodeHTML } from "entities";
import { Lexer } from "marked";
import type { MarkedToken, Token } from "marked";

/** A piece of inline Markdown: plain text, a code span, or a span of formatted pieces. */
export type InlineNode =
  | string
  | { kind: "code"; text: string }
  | { kind: "strong" | "em" | "del"; children: InlineNode[] };

/** Appends `node` to `nodes`, joining plain text to the plain text before it. */
const append = (nodes: InlineNode[], node: InlineNode) => {
  const last = nodes.at(-1);
  if (typeof node === "string" && typeof last === "string") {
    nodes[nodes.length - 1] = last + node;
  } else if (node !== "") {
    nodes.push(node);
  }
};

/** No marked extension is registered, so every token is one of marked's own. */
const isMarked = (_token: Token): _token is MarkedToken => true;

/** The pieces of `tokens`, plain text joined. */
const nodesOf = (tokens: Token[]): InlineNode[] => {
  const nodes: InlineNode[] = [];
  const add = (...pieces: InlineNode[]) => {
    for (const piece of pieces) {
      append(nodes, piece);
    }
  };
  for (const token of tokens.filter(isMarked)) {
    if (token.type === "codespan") {
      add({ kind: "code", text: token.text });
    } else if (
      token.type === "strong" ||
      token.type === "em" ||
      token.type === "del"
    ) {
      add({ children: nodesOf(token.tokens), kind: token.type });
    } else if (token.type === "link" || token.type === "image") {
      add(...nodesOf(token.tokens));
    } else if (token.type === "br") {
      add(" ");
    } else if (token.type === "text") {
      add(
        ...(token.tokens === undefined
          ? [decodeHTML(token.text)]
          : nodesOf(token.tokens))
      );
    } else if (token.type === "escape") {
      add(token.text);
    } else {
      // HTML and anything else inline shows as written.
      add(token.raw);
    }
  }
  return nodes;
};

/** The inline Markdown of `text` as pieces to render. */
export const parseInlineMarkdown = (text: string): InlineNode[] =>
  nodesOf(Lexer.lexInline(text, { gfm: true }));

const plain = (nodes: InlineNode[]): string =>
  nodes
    .map((node) => {
      if (typeof node === "string") {
        return node;
      }
      return node.kind === "code" ? node.text : plain(node.children);
    })
    .join("");

/** The text of `text` without its Markdown, for an accessible name or a tooltip. */
export const markdownText = (text: string) => plain(parseInlineMarkdown(text));
