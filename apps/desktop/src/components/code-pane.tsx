import { cva } from "class-variance-authority";

// The Code pane (DESIGN.md §6 "Code pane"): a file's lines in a grid of line number, marker and
// text, in `code.bg`, `type.code`, at most `code.max-height` high and scrolling past it. The tints
// of a change: an added line with "+" on `diff.added-bg`, a removed line with "−" on
// `diff.removed-bg` and no number. The whole rendering of a file and of a diff is here, from the
// file's content and the unified diff git wrote, so that another renderer could take its place
// without touching a screen.

/** How a line is shown: as it is, added, or removed. */
export type LineKind = "plain" | "added" | "removed";

export interface CodeLine {
  kind: LineKind;
  /** The line's number in the file shown; a removed line has none. */
  number?: number;
  text: string;
}

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
      added: "text-accent-attention",
      plain: "",
      removed: "text-accent-danger",
    },
  },
});

const textVariants = cva("break-all whitespace-pre-wrap no-underline", {
  variants: {
    tone: {
      frontmatter: "text-code-frontmatter",
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
        <ins className={textVariants({ tone })}>{text}</ins>
      )}
      {line.kind === "removed" && (
        <del className={textVariants({ tone })}>{text}</del>
      )}
      {line.kind === "plain" && (
        <span className={textVariants({ tone })}>{text}</span>
      )}
    </div>
  );
};

export interface CodePaneProps {
  /** The file's path, which sets how its lines are coloured. */
  path: string;
  /** The pane's accessible name. */
  label: string;
  /** The file's content as it is; absent for a deleted file. */
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
    return [];
  }
  if (tint === "added") {
    return numbered(content, "added");
  }
  return patch === undefined
    ? numbered(content, "plain")
    : editedLines(content, patch);
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
