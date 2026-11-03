import { cva } from "class-variance-authority";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";

import type { DiffEvent, SkillFileEvent } from "#/cli/events.gen.ts";
import { CodePane } from "#/components/code-pane.tsx";
import { FolderIcon } from "#/components/icons/folder.tsx";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "#/components/ui/accordion.tsx";
import { Button } from "#/components/ui/button.tsx";
import { revealInFinder } from "#/lib/reveal.ts";
import {
  filesRequestOf,
  filesVersionOf,
  skillDirectoryOf,
} from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { loadFiles } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";
import type { FilesRead } from "#/store/store.ts";

const SKILL_MD = "SKILL.md";

/** A file's size in words: bytes under a KiB, then KiB, then MiB with one decimal. */
export const formatSize = (bytes: number) => {
  if (bytes < 1024) {
    return bytes === 1 ? "1 byte" : `${bytes} bytes`;
  }
  if (bytes < 1024 * 1024) {
    return `${Math.round(bytes / 1024)} KiB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
};

const lines = (n: number) => (n === 1 ? "1 line" : `${n} lines`);

/** One row of the list: a file the CLI read, or one the skill's edits deleted, known from its diff. */
interface FileRow {
  path: string;
  file?: SkillFileEvent;
  diff?: DiffEvent;
}

const labels: Record<DiffEvent["status"], string> = {
  added: "added by you",
  deleted: "removed by you",
  modified: "edited by you",
};

/** What the row says beside the path: the line count, "binary", or that the file is too large. */
const metaOf = (row: FileRow) => {
  const { file } = row;
  if (file === undefined) {
    // A deleted file: the lines of its old content, from the patch.
    const removed = (row.diff?.patch ?? "")
      .split("\n")
      .filter((l) => l.startsWith("-") && !l.startsWith("---")).length;
    return lines(removed);
  }
  if (file.truncated === true) {
    return `Too large to show (${formatSize(file.size)})`;
  }
  const text = file.binary ? "binary" : lines(file.lines ?? 0);
  return file.ignored === true ? `${text} · ignored` : text;
};

/** SKILL.md first, then every other path in the CLI's order, the deleted files among them. */
const rowsOf = (read: FilesRead & { failed?: undefined }): FileRow[] => {
  const byPath = new Map<string, FileRow>();
  for (const file of read.files) {
    byPath.set(file.path, { file, path: file.path });
  }
  for (const diff of read.diffs ?? []) {
    const row = byPath.get(diff.path);
    if (row === undefined) {
      if (diff.status === "deleted") {
        byPath.set(diff.path, { diff, path: diff.path });
      }
    } else {
      row.diff = diff;
    }
  }
  const rows = [...byPath.values()];
  rows.sort((a, b) => {
    if (a.path === SKILL_MD || b.path === SKILL_MD) {
      return a.path === SKILL_MD ? -1 : 1;
    }
    if (a.path === b.path) {
      return 0;
    }
    return a.path < b.path ? -1 : 1;
  });
  return rows;
};

const pathVariants = cva("type-technical min-w-0 truncate", {
  variants: {
    open: {
      false: "text-file-row-path",
      true: "text-file-row-path-open",
    },
  },
});

/** The pane of one row: the file, with its edits tinted, or the placeholder for what is not shown. */
const Pane = ({ row }: { row: FileRow }) => {
  const { file, diff } = row;
  if (file === undefined) {
    return (
      <CodePane
        path={row.path}
        label={`Content of ${row.path}`}
        patch={diff?.patch ?? ""}
        tint="removed"
      />
    );
  }
  if (file.truncated === true) {
    return (
      <p className="bg-code-bg py-code px-file-row type-secondary text-text-hint m-0">
        {`Too large to show (${formatSize(file.size)})`}
      </p>
    );
  }
  if (file.binary) {
    return (
      <p className="bg-code-bg py-code px-file-row type-secondary text-text-hint m-0">
        {`Binary file (${formatSize(file.size)})`}
      </p>
    );
  }
  const content = file.content ?? "";
  if (diff?.status === "added") {
    return (
      <CodePane
        path={row.path}
        label={`Content of ${row.path}`}
        content={content}
        tint="added"
      />
    );
  }
  return diff?.status === "modified" ? (
    <CodePane
      path={row.path}
      label={`Content of ${row.path}`}
      content={content}
      patch={diff.patch}
    />
  ) : (
    <CodePane
      path={row.path}
      label={`Content of ${row.path}`}
      content={content}
    />
  );
};

const Hint = ({ children }: { children: string }) => (
  <p className="type-secondary text-text-hint m-0">{children}</p>
);

/** What the edits of a modified skill are compared with, by what the skill is. */
const comparedWith = (row: SkillRow) =>
  row.kind === "yours"
    ? "Compared with what you last published"
    : "Compared with the version you installed";

/**
 * FILES in the detail pane: every file of the skill from `skill files`, read when the skill is
 * selected, as an accordion with SKILL.md first and open. For a modified skill, `skill diff
 * <name>` says which files were edited, added or removed since the version it is compared with,
 * and each pane shows the edits tinted. `heading` is the section's caption.
 */
export const FilesSection = ({
  row,
  heading,
}: {
  row: SkillRow;
  heading: ReactNode;
}) => {
  const request = filesRequestOf(row);
  const version = filesVersionOf(row);
  const directory = skillDirectoryOf(row);
  const read = useAppStore((s) => s.files[row.key]);
  const [open, setOpen] = useState<string[]>([SKILL_MD]);

  useEffect(() => {
    const wanted = filesRequestOf(row);
    if (wanted !== undefined) {
      void loadFiles(row.key, wanted, version);
    }
  }, [row, version]);

  const current =
    read !== undefined && read.version === version ? read : undefined;
  const compared =
    current?.failed === undefined && current?.diffs !== undefined
      ? comparedWith(row)
      : undefined;

  let body: ReactNode;
  if (request === undefined) {
    body = <Hint>No files to show</Hint>;
  } else if (current === undefined) {
    body = <Hint>Reading the files…</Hint>;
  } else if (current.failed === true) {
    body = <Hint>The files could not be read</Hint>;
  } else {
    const rows = rowsOf(current);
    body =
      rows.length === 0 ? (
        <Hint>No files</Hint>
      ) : (
        <Accordion label="Files" value={open} onValueChange={setOpen}>
          {rows.map((file) => {
            const isOpen = open.includes(file.path);
            return (
              <AccordionItem key={file.path} value={file.path}>
                <AccordionTrigger
                  label={file.path}
                  open={isOpen}
                  action={
                    // A file on disk can be shown where it is; a deleted file has no place.
                    file.file !== undefined && directory !== undefined ? (
                      <Button
                        variant="ghost"
                        size="icon"
                        aria-label={`Show ${file.path} in Finder`}
                        title="Show in Finder"
                        onClick={() => {
                          void revealInFinder(`${directory}/${file.path}`);
                        }}
                      >
                        <FolderIcon />
                      </Button>
                    ) : undefined
                  }
                >
                  <span className={pathVariants({ open: isOpen })}>
                    {file.path}
                  </span>
                  {file.diff !== undefined && (
                    <span className="type-technical-sm text-file-row-edited-label flex-none">
                      {labels[file.diff.status]}
                    </span>
                  )}
                  <span className="type-technical-sm text-file-row-meta ml-auto flex-none">
                    {metaOf(file)}
                  </span>
                </AccordionTrigger>
                <AccordionContent>
                  <Pane row={file} />
                </AccordionContent>
              </AccordionItem>
            );
          })}
        </Accordion>
      );
  }

  return (
    <section aria-label="Files" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline gap-3">
        {heading}
        <span className="type-technical-sm text-text-caption">Read-only</span>
        {compared !== undefined && (
          <span className="type-technical-sm text-text-caption">
            {compared}
          </span>
        )}
        {current?.failed === undefined && current?.diffFailed === true && (
          <span className="type-technical-sm text-text-caption">
            The edits could not be read
          </span>
        )}
      </div>
      {body}
    </section>
  );
};
