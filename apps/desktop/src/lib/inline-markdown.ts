// The inline Markdown of a one-line text, such as a skill's description from its SKILL.md
// frontmatter: code spans, strong, emphasis and strikethrough. A link shows its text and an
// autolink its URL, as a description sits inside a row that opens on a click. Block syntax
// (headings, lists, quotes) stays as written, as the text is one line. The rules follow
// CommonMark closely enough for a description: a backslash escapes the next punctuation mark, a
// code span ends at a run of as many backticks as opened it, a delimiter opens before a
// non-space and closes after one, and `_` never opens or closes inside a word, so a name such
// as `snake_case_name` stays as it is.

/** A piece of inline Markdown: plain text, a code span, or a span of formatted pieces. */
export type InlineNode =
  | string
  | { kind: "code"; text: string }
  | { kind: "strong" | "em" | "del"; children: InlineNode[] };

/** The delimiters of a formatted span, longest first, so `**` is read before `*`. */
const delimiters = [
  ["**", "strong"],
  ["__", "strong"],
  ["~~", "del"],
  ["*", "em"],
  ["_", "em"],
] as const;

const punctuation = /[!-/:-@[-`{-~]/u;
const space = /\s/u;
const wordChar = /[\p{L}\p{N}]/u;

const isSpace = (char: string | undefined) =>
  char === undefined || space.test(char);
const isWord = (char: string | undefined) =>
  char !== undefined && wordChar.test(char);

/** The end of the code span that opens at `start`, after its closing run, or -1 when none closes it. */
const codeEnd = (text: string, start: number) => {
  let run = 0;
  while (text[start + run] === "`") {
    run += 1;
  }
  let at = start + run;
  while (at < text.length) {
    if (text[at] === "`") {
      let close = 0;
      while (text[at + close] === "`") {
        close += 1;
      }
      if (close === run) {
        return at + close;
      }
      at += close;
    } else {
      at += 1;
    }
  }
  return -1;
};

/** Whether `marker` at `at` can open a span. */
const opens = (text: string, at: number, marker: string) =>
  !isSpace(text[at + marker.length]) &&
  !(marker.startsWith("_") && isWord(text[at - 1]));

/** Whether `marker` at `at` can close a span. */
const closes = (text: string, at: number, marker: string) =>
  !isSpace(text[at - 1]) &&
  !(marker.startsWith("_") && isWord(text[at + marker.length]));

/**
 * Where the span that `marker` opens at `start` closes, or -1. The search steps over escapes and
 * code spans, and a single `*` or `_` steps over a doubled one, which is a nested strong span.
 */
const closeOf = (text: string, start: number, marker: string) => {
  let at = start + marker.length + 1;
  while (at < text.length) {
    const char = text[at];
    if (char === "\\") {
      at += 2;
    } else if (char === "`") {
      const end = codeEnd(text, at);
      at = end === -1 ? at + 1 : end;
    } else if (text.startsWith(marker, at)) {
      const doubled =
        marker.length === 1 &&
        text[at + 1] === marker &&
        text[at - 1] !== marker;
      if (doubled) {
        at += 2;
      } else if (closes(text, at, marker)) {
        return at;
      } else {
        at += marker.length;
      }
    } else {
      at += 1;
    }
  }
  return -1;
};

/** What a reader found at a position: the pieces it reads as, and where it ends. */
interface Read {
  nodes: InlineNode[];
  end: number;
}

/** Reads the inline Markdown inside a span or a link's text. */
type Parse = (text: string) => InlineNode[];

/** A backslash before a punctuation mark: the mark as text. */
const readEscape = (text: string, at: number): Read | undefined => {
  const next = text[at + 1];
  return text[at] === "\\" && next !== undefined && punctuation.test(next)
    ? { end: at + 2, nodes: [next] }
    : undefined;
};

/** A code span, or its opening run of backticks as text when nothing closes it. */
const readCode = (text: string, at: number): Read | undefined => {
  if (text[at] !== "`") {
    return undefined;
  }
  let run = 0;
  while (text[at + run] === "`") {
    run += 1;
  }
  const end = codeEnd(text, at);
  if (end === -1) {
    return { end: at + run, nodes: ["`".repeat(run)] };
  }
  const inner = text.slice(at + run, end - run);
  const padded =
    inner.length > 2 &&
    inner.startsWith(" ") &&
    inner.endsWith(" ") &&
    inner.trim() !== "";
  return {
    end,
    nodes: [{ kind: "code", text: padded ? inner.slice(1, -1) : inner }],
  };
};

/** A link `[text](url)`: its text, read as inline Markdown. */
const readLink = (text: string, at: number, parse: Parse): Read | undefined => {
  if (text[at] !== "[") {
    return undefined;
  }
  let depth = 0;
  let close = at;
  for (; close < text.length; close += 1) {
    const char = text[close];
    if (char === "\\") {
      close += 1;
    } else if (char === "[") {
      depth += 1;
    } else if (char === "]") {
      depth -= 1;
      if (depth === 0) {
        break;
      }
    }
  }
  const urlEnd = text.indexOf(")", close + 2);
  if (
    depth !== 0 ||
    text[close + 1] !== "(" ||
    urlEnd === -1 ||
    space.test(text.slice(close + 2, urlEnd).trim())
  ) {
    return undefined;
  }
  return {
    end: urlEnd + 1,
    nodes: parse(text.slice(at + 1, close)),
  };
};

const autolink = /^<(?<url>[a-z][\w+.-]*:[^\s<>]+)>/iu;

/** An autolink `<https://…>`: its URL as text. */
const readAutolink = (text: string, at: number): Read | undefined => {
  const match = text[at] === "<" ? autolink.exec(text.slice(at)) : null;
  return match === null
    ? undefined
    : { end: at + match[0].length, nodes: [match.groups?.url ?? ""] };
};

/** A formatted span, or its delimiter as text when it cannot open one or nothing closes it. */
const readSpan = (text: string, at: number, parse: Parse): Read | undefined => {
  const delimiter = delimiters.find(([marker]) => text.startsWith(marker, at));
  if (delimiter === undefined) {
    return undefined;
  }
  const [marker, kind] = delimiter;
  const close = opens(text, at, marker) ? closeOf(text, at, marker) : -1;
  if (close === -1) {
    return { end: at + marker.length, nodes: [marker] };
  }
  return {
    end: close + marker.length,
    nodes: [
      {
        children: parse(text.slice(at + marker.length, close)),
        kind,
      },
    ],
  };
};

const readers = [readEscape, readCode, readLink, readAutolink, readSpan];

/** The inline Markdown of `text` as pieces to render. */
export const parseInlineMarkdown = (text: string): InlineNode[] => {
  const nodes: InlineNode[] = [];
  const append = (node: InlineNode) => {
    const last = nodes.at(-1);
    if (typeof node === "string" && typeof last === "string") {
      nodes[nodes.length - 1] = last + node;
    } else {
      nodes.push(node);
    }
  };
  let at = 0;
  while (at < text.length) {
    let read: Read | undefined;
    for (const reader of readers) {
      read ??= reader(text, at, parseInlineMarkdown);
    }
    if (read === undefined) {
      append(text[at] ?? "");
      at += 1;
    } else {
      for (const node of read.nodes) {
        append(node);
      }
      at = read.end;
    }
  }
  return nodes;
};

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
