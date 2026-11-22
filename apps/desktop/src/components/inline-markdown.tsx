// The inline Markdown of a one-line text, such as a skill's description (DESIGN.md §6 "Inline
// Markdown"). It renders inline elements only, so the caller's span sets the type, colour and
// any truncation or line clamp.
import type { ReactNode } from "react";

import { parseInlineMarkdown } from "#/lib/inline-markdown.ts";
import type { InlineNode } from "#/lib/inline-markdown.ts";

const render = (nodes: InlineNode[]): ReactNode[] =>
  nodes.map((node, index) => {
    const key = `${index}`;
    if (typeof node === "string") {
      return node;
    }
    if (node.kind === "code") {
      return (
        <code
          key={key}
          className="type-markdown-code text-markdown-code-text bg-markdown-code-bg rounded-markdown-code px-1"
        >
          {node.text}
        </code>
      );
    }
    if (node.kind === "strong") {
      return (
        <strong key={key} className="text-markdown-strong font-semibold">
          {render(node.children)}
        </strong>
      );
    }
    if (node.kind === "em") {
      return (
        <em key={key} className="italic">
          {render(node.children)}
        </em>
      );
    }
    return (
      <del key={key} className="line-through">
        {render(node.children)}
      </del>
    );
  });

/** `text` with its inline Markdown rendered: code spans, strong, emphasis, strikethrough, link text. */
export const InlineMarkdown = ({ text }: { text: string }) =>
  render(parseInlineMarkdown(text));
