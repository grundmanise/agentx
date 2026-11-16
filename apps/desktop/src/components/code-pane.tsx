import { cva } from "class-variance-authority";

// The Code pane (DESIGN.md §6 "Code pane"): a file's lines in a grid of line number, marker and
// text, in `code.bg`, `type.code`, at most `code.max-height` high and scrolling past it. The tints
// of a change: an added line with "+" in `accent.primary` on `diff.added-bg`, the palette's green,
// a removed line with "−" in `accent.danger` on
// `diff.removed-bg` and no number. A removed line and the added line that replaced it also mark
// the words that changed between them, on `diff.removed-word` and `diff.added-word`. A patch with
// no file content shows its hunks alone. The whole rendering of a file and of a diff is here, from
// the file's content and the unified diff git wrote, so that another renderer could take its
// place without touching a screen.

/** How a line is shown: as it is, added, or removed. */
export type LineKind = "plain" | "added" | "removed";

/** A run of a changed line's text: kept from the other line of its pair, or changed. */
export interface Segment {
  changed: boolean;
  text: string;
}

export interface CodeLine {
  kind: LineKind;
  /** The line's number in the file shown; a removed line has none. */
  number?: number;
  text: string;
  /**
   * The line's text cut where it differs from the line it pairs with: a removed line and the
   * added line that replaced it. Absent for a line with no pair, or one too unlike its pair.
   */
  segments?: Segment[];
}

/** A word, a run of white space, or one other character: what the word diff compares. */
const TOKEN = /[\p{L}\p{N}_]+|\s+|[^\p{L}\p{N}_\s]/gu;
const SPACE = /^\s+$/u;
/** The token pairs above which a line pair is too long to compare and keeps its line tint. */
const MAX_CELLS = 250_000;
/**
 * The share of the shorter line, in characters other than white space, a pair must keep for its
 * changed words to be marked: under it, the lines are unlike, and the whole line is the change.
 */
const MIN_KEPT = 0.5;

/** The characters of `tokens` other than white space, counting only those `keep` marks. */
const nonSpaceLength = (
  tokens: readonly string[],
  keep: (i: number) => boolean
) => {
  let sum = 0;
  for (const [i, token] of tokens.entries()) {
    if (keep(i) && !SPACE.test(token)) {
      sum += token.length;
    }
  }
  return sum;
};

/** Joins runs of tokens with the same flag into segments. */
const segmentsOf = (
  tokens: readonly string[],
  changed: readonly boolean[]
): Segment[] => {
  const out: Segment[] = [];
  for (const [i, token] of tokens.entries()) {
    const flag = changed[i] ?? false;
    const last = out.at(-1);
    if (last?.changed === flag) {
      last.text += token;
    } else {
      out.push({ changed: flag, text: token });
    }
  }
  return out;
};

/** Marks white space kept between two changed tokens as changed, so a change reads as one run. */
const joinGaps = (tokens: readonly string[], changed: boolean[]) => {
  for (let i = 1; i < tokens.length - 1; i += 1) {
    if (
      !changed[i] &&
      changed[i - 1] &&
      changed[i + 1] &&
      SPACE.test(tokens[i] ?? "")
    ) {
      changed[i] = true;
    }
  }
};

/** How many tokens `a` and `b` share at their start, and then at their end. */
const commonEnds = (a: readonly string[], b: readonly string[]) => {
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) {
    start += 1;
  }
  let end = 0;
  while (
    end < a.length - start &&
    end < b.length - start &&
    a[a.length - 1 - end] === b[b.length - 1 - end]
  ) {
    end += 1;
  }
  return { end, start };
};

/**
 * Which tokens of `a` and of `b` are not in their longest common subsequence: the changed ones.
 * Undefined when the two are too long to compare.
 */
const changedTokens = (
  a: readonly string[],
  b: readonly string[]
): [boolean[], boolean[]] | undefined => {
  const n = a.length;
  const m = b.length;
  if (n * m > MAX_CELLS) {
    return undefined;
  }
  // lcs[i * w + j]: the length of the longest common subsequence of a[i..] and b[j..].
  const w = m + 1;
  const lcs = new Uint32Array((n + 1) * w);
  const at = (i: number, j: number) => lcs[i * w + j] ?? 0;
  for (let i = n - 1; i >= 0; i -= 1) {
    for (let j = m - 1; j >= 0; j -= 1) {
      lcs[i * w + j] =
        a[i] === b[j]
          ? at(i + 1, j + 1) + 1
          : Math.max(at(i + 1, j), at(i, j + 1));
    }
  }
  const changedA = a.map(() => true);
  const changedB = b.map(() => true);
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      changedA[i] = false;
      changedB[j] = false;
      i += 1;
      j += 1;
    } else if (at(i + 1, j) >= at(i, j + 1)) {
      i += 1;
    } else {
      j += 1;
    }
  }
  return [changedA, changedB];
};

/**
 * The words that differ between a removed line and the added line that replaced it: the longest
 * common run of tokens is kept, the rest changed. Undefined when the lines are too long to compare
 * or too unlike to mark.
 */
export const wordDiff = (
  before: string,
  after: string
): [Segment[], Segment[]] | undefined => {
  const a = before.match(TOKEN) ?? [];
  const b = after.match(TOKEN) ?? [];
  const { start, end } = commonEnds(a, b);
  const middle = changedTokens(
    a.slice(start, a.length - end),
    b.slice(start, b.length - end)
  );
  if (middle === undefined) {
    return undefined;
  }
  const changedA = a.map((_, i) => middle[0][i - start] ?? false);
  const changedB = b.map((_, j) => middle[1][j - start] ?? false);
  const kept = nonSpaceLength(a, (i) => !changedA[i]);
  const shorter = Math.min(
    nonSpaceLength(a, () => true),
    nonSpaceLength(b, () => true)
  );
  if (kept === 0 || kept < shorter * MIN_KEPT) {
    return undefined;
  }
  joinGaps(a, changedA);
  joinGaps(b, changedB);
  return [segmentsOf(a, changedA), segmentsOf(b, changedB)];
};

/**
 * The lines with the changed words of each pair marked. A run of removed lines followed by a run
 * of added lines is one change: its first removed line pairs with its first added line, and so
 * on, as git lays a replaced line out.
 */
const withWordDiffs = (lines: readonly CodeLine[]): CodeLine[] => {
  const out = [...lines];
  let i = 0;
  while (i < out.length) {
    if (out[i]?.kind !== "removed") {
      i += 1;
      continue;
    }
    let added = i;
    while (out[added]?.kind === "removed") {
      added += 1;
    }
    let end = added;
    while (out[end]?.kind === "added") {
      end += 1;
    }
    const pairs = Math.min(added - i, end - added);
    for (let k = 0; k < pairs; k += 1) {
      const removed = out[i + k];
      const replacement = out[added + k];
      if (removed === undefined || replacement === undefined) {
        continue;
      }
      const diff = wordDiff(removed.text, replacement.text);
      if (diff !== undefined) {
        out[i + k] = { ...removed, segments: diff[0] };
        out[added + k] = { ...replacement, segments: diff[1] };
      }
    }
    i = Math.max(end, i + 1);
  }
  return out;
};

/** One line of a unified diff's hunk. */
interface PatchLine {
  kind: "context" | "added" | "removed";
  text: string;
}

interface Hunk {
  /** The first line of the hunk in the new file, 1-based; 0 when the new file has none. */
  newStart: number;
  lines: PatchLine[];
}

const HUNK_HEADER = /^@@ -\d+(?:,\d+)? \+(?<start>\d+)(?:,\d+)? @@/u;

/** The hunks of the unified diff git wrote for one file; the file header is skipped. */
export const parsePatch = (patch: string): Hunk[] => {
  const hunks: Hunk[] = [];
  let current: Hunk | undefined;
  for (const raw of patch.split("\n")) {
    const header = HUNK_HEADER.exec(raw);
    if (header?.groups !== undefined) {
      current = { lines: [], newStart: Number(header.groups.start) };
      hunks.push(current);
      continue;
    }
    if (current === undefined) {
      continue;
    }
    if (raw.startsWith("+")) {
      current.lines.push({ kind: "added", text: raw.slice(1) });
    } else if (raw.startsWith("-")) {
      current.lines.push({ kind: "removed", text: raw.slice(1) });
    } else if (raw.startsWith(" ")) {
      current.lines.push({ kind: "context", text: raw.slice(1) });
    }
    // "\ No newline at end of file" and the empty string after the final newline are no lines.
  }
  return hunks;
};

/** The lines of a file's content, as the CLI counts them: a last line without a newline counts. */
export const linesOf = (content: string): string[] => {
  const lines = content.split("\n");
  if (lines.at(-1) === "") {
    lines.pop();
  }
  return lines;
};

const numbered = (content: string, kind: LineKind): CodeLine[] =>
  linesOf(content).map((text, i) => ({ kind, number: i + 1, text }));

/** The old content of a deleted file: the removed lines of its patch. */
const removedLines = (patch: string): CodeLine[] =>
  parsePatch(patch)
    .flatMap((hunk) => hunk.lines)
    .filter((line) => line.kind === "removed")
    .map((line) => ({ kind: "removed", text: line.text }));

/**
 * A patch on its own, with no file to lay it on: the lines of each hunk in order, those it added
 * and kept numbered as in the new file, those it removed tinted and unnumbered.
 */
const hunkLines = (patch: string): CodeLine[] =>
  parsePatch(patch).flatMap((hunk) => {
    let next = hunk.newStart;
    return hunk.lines.map((line): CodeLine => {
      if (line.kind === "removed") {
        return { kind: "removed", text: line.text };
      }
      const number = next;
      next += 1;
      return {
        kind: line.kind === "added" ? "added" : "plain",
        number,
        text: line.text,
      };
    });
  });

/**
 * The file as it is, `content`, with the lines its patch removed put back where they were, each
 * tinted as removed, and the lines it added tinted as added. A patch with no hunk shows the file
 * as it is.
 */
const editedLines = (content: string, patch: string): CodeLine[] => {
  const lines = linesOf(content);
  const hunks = parsePatch(patch);
  if (hunks.length === 0) {
    return numbered(content, "plain");
  }
  const out: CodeLine[] = [];
  let next = 1;
  const emit = (kind: LineKind, fallback: string) => {
    out.push({ kind, number: next, text: lines[next - 1] ?? fallback });
    next += 1;
  };
  for (const hunk of hunks) {
    while (next < hunk.newStart && next <= lines.length) {
      emit("plain", "");
    }
    for (const line of hunk.lines) {
      if (line.kind === "removed") {
        out.push({ kind: "removed", text: line.text });
      } else {
        emit(line.kind === "added" ? "added" : "plain", line.text);
      }
    }
  }
  while (next <= lines.length) {
    emit("plain", "");
  }
  return out;
};

/** What a line of a Markdown file is, which sets its colour. */
type Tone = "frontmatter" | "heading" | "list" | "prose";

const MARKDOWN = /\.mdx?$/iu;
const HEADING = /^#{1,6}\s/u;
const LIST_ITEM = /^\s*(?:[-*+]|\d+[.)])\s/u;

/** The tone of each line: the front-matter block, headings and list items of a Markdown file. */
const tonesOf = (lines: readonly CodeLine[], path: string): Tone[] => {
  if (!MARKDOWN.test(path)) {
    return lines.map(() => "prose");
  }
  let inFrontmatter = false;
  let first = true;
  return lines.map((line) => {
    if (first) {
      first = false;
      if (line.text === "---") {
        inFrontmatter = true;
        return "frontmatter";
      }
    } else if (inFrontmatter) {
      if (line.text === "---") {
        inFrontmatter = false;
      }
      return "frontmatter";
    }
    if (HEADING.test(line.text)) {
      return "heading";
    }
    return LIST_ITEM.test(line.text) ? "list" : "prose";
  });
};

const lineVariants = cva("grid-cols-code gap-x-code grid", {
  variants: {
    kind: {
      added: "bg-diff-added-bg",
      plain: "",
      removed: "bg-diff-removed-bg",
    },
  },
});

const markerVariants = cva("text-center", {
  variants: {
    kind: {
      added: "text-accent-primary",
      plain: "",
      removed: "text-accent-danger",
    },
  },
});

// On a tinted line, front matter takes `code.prose`: `code.frontmatter` on the added tint falls
// under the 4.5:1 contrast text needs (DESIGN.md §6 "Code pane").
const textVariants = cva("break-all whitespace-pre-wrap no-underline", {
  compoundVariants: [
    { className: "text-code-frontmatter", kind: "plain", tone: "frontmatter" },
    {
      className: "text-code-prose",
      kind: ["added", "removed"],
      tone: "frontmatter",
    },
  ],
  variants: {
    kind: {
      added: "",
      plain: "",
      removed: "",
    },
    tone: {
      frontmatter: "",
      heading: "text-code-heading",
      list: "text-code-list",
      prose: "text-code-prose",
    },
  },
});

const markers: Record<LineKind, string> = {
  added: "+",
  plain: "",
  removed: "−",
};

// A changed word's text is `code.strong`: the line's own tone falls under 4.5:1 on the word tints
// (DESIGN.md §6 "Code pane").
const wordVariants = cva("text-code-strong", {
  variants: {
    kind: {
      added: "bg-diff-added-word",
      plain: "",
      removed: "bg-diff-removed-word",
    },
  },
});

/** A changed line's text, its changed words marked when it pairs with the line it replaced. */
const LineText = ({ line, text }: { line: CodeLine; text: string }) => {
  if (line.segments === undefined) {
    return text;
  }
  return line.segments.map((segment, i) =>
    segment.changed ? (
      <mark key={i} className={wordVariants({ kind: line.kind })}>
        {segment.text}
      </mark>
    ) : (
      segment.text
    )
  );
};

const Line = ({ line, tone }: { line: CodeLine; tone: Tone }) => {
  const text = line.text === "" ? " " : line.text;
  return (
    <div className={lineVariants({ kind: line.kind })}>
      <span aria-hidden className="text-code-line-number text-right">
        {line.number}
      </span>
      <span aria-hidden className={markerVariants({ kind: line.kind })}>
        {markers[line.kind]}
      </span>
      {line.kind === "added" && (
        <ins className={textVariants({ kind: "added", tone })}>
          <LineText line={line} text={text} />
        </ins>
      )}
      {line.kind === "removed" && (
        <del className={textVariants({ kind: "removed", tone })}>
          <LineText line={line} text={text} />
        </del>
      )}
      {line.kind === "plain" && (
        <span className={textVariants({ kind: "plain", tone })}>{text}</span>
      )}
    </div>
  );
};

export interface CodePaneProps {
  /** The file's path, which sets how its lines are coloured. */
  path: string;
  /** The pane's accessible name. */
  label: string;
  /**
   * The file's content as it is; absent for a deleted file, and for a patch shown on its own,
   * such as what an update changes, whose hunks are then all the pane shows.
   */
  content?: string;
  /** The unified diff git wrote for the file, whose tints the pane shows. */
  patch?: string;
  /**
   * The tint of the whole file: "added" for a file that is new, "removed" for one deleted, whose
   * old content comes from `patch`. Absent, a file with a patch shows the patch's tints.
   */
  tint?: "added" | "removed";
}

/** The lines the pane shows for `props`. */
export const codeLines = ({
  content,
  patch,
  tint,
}: Pick<CodePaneProps, "content" | "patch" | "tint">): CodeLine[] => {
  if (tint === "removed") {
    return patch === undefined ? [] : removedLines(patch);
  }
  if (content === undefined) {
    return patch === undefined || tint !== undefined
      ? []
      : withWordDiffs(hunkLines(patch));
  }
  if (tint === "added") {
    return numbered(content, "added");
  }
  return patch === undefined
    ? numbered(content, "plain")
    : withWordDiffs(editedLines(content, patch));
};

export const CodePane = (props: CodePaneProps) => {
  const lines = codeLines(props);
  const tones = tonesOf(lines, props.path);
  return (
    <section
      aria-label={props.label}
      className="bg-code-bg max-h-code py-code type-code overflow-auto"
    >
      {lines.length === 0 ? (
        <p className="px-file-row type-secondary text-text-hint m-0">
          Empty file
        </p>
      ) : (
        lines.map((line, i) => (
          <Line
            key={`${line.number ?? "r"}-${i}`}
            line={line}
            tone={tones[i] ?? "prose"}
          />
        ))
      )}
    </section>
  );
};
