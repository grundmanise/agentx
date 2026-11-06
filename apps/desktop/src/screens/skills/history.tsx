import { cva } from "class-variance-authority";
import { useEffect, useState } from "react";
import type { ReactNode } from "react";

import type { DiffEvent, HistoryEvent } from "#/cli/events.gen.ts";
import { CodePane, parsePatch } from "#/components/code-pane.tsx";
import { ChevronIcon } from "#/components/icons/chevron.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "#/components/ui/sheet.tsx";
import { formatDate } from "#/lib/dates.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { compareLabel, loadComparison, loadHistory } from "#/store/loaders.ts";
import { usePending } from "#/store/runner.ts";
import { comparisonKey, useAppStore } from "#/store/store.ts";

/** How many entries the timeline shows before "Show all". */
const SHOWN = 3;

const dotVariants = cva("size-timeline-dot rounded-pill block", {
  variants: {
    kind: {
      foreign: "bg-timeline-dot-update",
      import: "bg-timeline-dot-install",
      yours: "bg-timeline-dot-yours",
    },
  },
});

const kindOf = (e: HistoryEvent) => {
  if (e.import) {
    return "import";
  }
  return e.foreign ? "foreign" : "yours";
};

const files = (n: number) => (n === 1 ? "1 file" : `${n} files`);

/**
 * The dot column of a timeline entry: `children` (the dot) centred in a box as high as the
 * entry's first line, a line of text or the first line of a wrapping small button, then the
 * connector down to the next entry, so every dot sits on its entry's first line and the line
 * stays continuous.
 */
const DotColumn = ({
  button,
  last,
  children,
}: {
  /** The first line beside the dot is the first line of a wrapping small button, not plain text. */
  button: boolean;
  last: boolean;
  children: ReactNode;
}) => (
  <span className="flex flex-col items-center">
    <span
      className={
        button
          ? "h-timeline-button-line flex flex-none items-center"
          : "h-timeline-line flex flex-none items-center"
      }
    >
      {children}
    </span>
    {!last && <span className="bg-timeline-connector w-px flex-1" />}
  </span>
);

/** What the meta line of an entry says: the date, the files, and how the commit came about. */
export const metaOf = (e: HistoryEvent) => {
  const parts = [formatDate(e.time), files(e.files.length)];
  if (e.foreign) {
    parts.push("made with Git");
  } else if (e.import) {
    parts.push("from the original");
  }
  return parts.join(" · ");
};

/** The version a skill's history is read for: a new read when the skill changed. */
const versionOf = (row: SkillRow) =>
  [
    row.entry?.content_hash ?? "",
    row.entry?.state ?? "",
    ...(row.entry?.drift ?? []),
  ].join(":");

/** One file of a comparison: its status and path, then its patch with the diff tints. */
const ComparedFile = ({ diff }: { diff: DiffEvent }) => (
  <li className="flex flex-col gap-1">
    <span className="flex items-baseline gap-2">
      <span className="type-technical-sm text-text-caption flex-none">
        {diff.status}
      </span>
      <span className="type-technical-sm text-text-secondary break-all">
        {diff.path}
      </span>
    </span>
    {parsePatch(diff.patch).length === 0 ? (
      // A binary file, or a change git writes no lines for, such as a file's mode.
      <p className="bg-code-bg py-code px-file-row type-secondary text-text-hint m-0">
        No lines to show
      </p>
    ) : (
      <CodePane
        path={diff.path}
        label={`Changes to ${diff.path}`}
        patch={diff.patch}
      />
    )}
  </li>
);

/**
 * Compare with now under an expanded entry of the sheet: `skill diff <name> --commit <commit>`,
 * run when asked, then one block per file that differs between the commit and the skill as it is
 * now. A comparison of another version of the skill is not shown; a failed one says why and runs
 * again on Retry.
 */
const Comparison = ({
  name,
  version,
  entry,
}: {
  name: string;
  /** The skill as it is now, as `versionOf` names it. */
  version: string;
  entry: HistoryEvent;
}) => {
  const comparing = usePending(compareLabel(name, entry.commit));
  const read = useAppStore(
    (s) => s.comparisons[comparisonKey(name, entry.commit)]
  );
  const current = read?.version === version ? read : undefined;
  const compare = () => {
    void loadComparison(name, entry.commit, version);
  };

  if (current !== undefined && current.failed === undefined) {
    const { diffs } = current;
    return (
      <section
        aria-label={`${entry.subject} compared with now`}
        className="flex flex-col gap-2"
      >
        <span className="type-technical-sm text-text-caption">
          {diffs.length === 0
            ? "Matches the skill as it is now"
            : `${files(diffs.length)} changed since this version`}
        </span>
        {diffs.length > 0 && (
          <ul className="gap-hunk m-0 flex list-none flex-col p-0">
            {diffs.map((d) => (
              <ComparedFile key={d.path} diff={d} />
            ))}
          </ul>
        )}
      </section>
    );
  }
  return (
    <div className="flex flex-col items-start gap-2">
      {current?.failed === true && (
        <div role="alert" className="flex flex-col gap-1">
          <p className="type-notice text-tone-danger-fg m-0">
            {current.problem.message}
          </p>
          {current.problem.hint !== undefined && (
            <p className="type-notice text-text-muted m-0">
              {current.problem.hint}
            </p>
          )}
        </div>
      )}
      {/* Aligned with the subject's text, as the entry's button is. */}
      <div className="-ml-3.5">
        <Button
          variant="ghost"
          size="sm"
          disabled={comparing}
          onClick={compare}
        >
          {comparing && <Spinner />}
          {current?.failed === true ? "Retry" : "Compare with now"}
        </Button>
      </div>
    </div>
  );
};

const Entry = ({
  entry,
  last,
  expandable = false,
  compare,
}: {
  entry: HistoryEvent;
  last: boolean;
  /** Shows a chevron that opens the files the commit changed. */
  expandable?: boolean;
  /** The skill the open entry compares with now, and its version: Compare with now under the files. */
  compare?: { name: string; version: string };
}) => {
  const [open, setOpen] = useState(false);
  return (
    <li className="grid-cols-timeline grid gap-x-3">
      <DotColumn button={expandable} last={last}>
        <span className={dotVariants({ kind: kindOf(entry) })} />
      </DotColumn>
      <div
        className={
          last ? "flex min-w-0 flex-col" : "flex min-w-0 flex-col pb-4"
        }
      >
        {expandable ? (
          // A block, so the button shrinks to fit the column and its subject wraps inside it.
          <div className="-ml-3.5">
            <Button
              variant="ghost"
              size="sm-wrap"
              aria-expanded={open}
              onClick={() => {
                setOpen((o) => !o);
              }}
            >
              <span className="h-timeline-line flex flex-none items-center">
                <span className={open ? "flex rotate-90" : "flex"}>
                  <ChevronIcon size="xs" />
                </span>
              </span>
              <span className="type-timeline-entry text-text-primary">
                {entry.subject}
              </span>
            </Button>
          </div>
        ) : (
          <span className="type-timeline-entry text-text-primary">
            {entry.subject}
          </span>
        )}
        <span className="type-timeline-meta text-timeline-meta">
          {metaOf(entry)}
        </span>
        {expandable && open && (
          <ul
            aria-label={`Files of ${entry.subject}`}
            className="m-0 mt-2 flex list-none flex-col gap-1 p-0"
          >
            {entry.files.map((f) => (
              <li key={f.path} className="flex items-baseline gap-2">
                <span className="type-technical-sm text-text-caption flex-none">
                  {f.status}
                </span>
                <span className="type-technical-sm text-text-secondary break-all">
                  {f.path}
                </span>
              </li>
            ))}
          </ul>
        )}
        {expandable && open && compare !== undefined && (
          <div className="mt-3">
            <Comparison
              name={compare.name}
              version={compare.version}
              entry={entry}
            />
          </div>
        )}
      </div>
    </li>
  );
};

/**
 * The History sheet: every commit of the skill, each expandable to the files it changed and a
 * comparison of the commit with the skill as it is now.
 */
const HistorySheet = ({
  name,
  version,
  entries,
  open,
  onOpenChange,
}: {
  name: string;
  /** The skill as it is now, which Compare with now compares a commit with. */
  version: string;
  entries: HistoryEvent[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => (
  <Sheet open={open} onOpenChange={onOpenChange}>
    <SheetContent>
      <SheetHeader>
        <SheetTitle>History</SheetTitle>
        <SheetDescription>{name}</SheetDescription>
      </SheetHeader>
      {/* The entries scroll down only; a long subject wraps. */}
      <div className="min-h-0 flex-1 overflow-auto">
        <ol
          aria-label="All history"
          className="m-0 flex list-none flex-col p-0"
        >
          {entries.map((e, i) => (
            <Entry
              key={e.commit}
              entry={e}
              last={i === entries.length - 1}
              expandable
              compare={{ name, version }}
            />
          ))}
        </ol>
      </div>
    </SheetContent>
  </Sheet>
);

/**
 * HISTORY in the detail pane. For one of your own skills, the latest commits of its branch from
 * `skill history <name>`, read when the skill is selected; "Show all" opens the sheet. Other
 * skills have no history of their own.
 */
export const HistorySection = ({ row }: { row: SkillRow }) => {
  const yours = row.kind === "yours";
  const version = versionOf(row);
  const read = useAppStore((s) => s.histories[row.name]);
  const [sheetOpen, setSheetOpen] = useState(false);

  useEffect(() => {
    if (yours) {
      void loadHistory(row.name, version);
    }
  }, [yours, row.name, version]);

  if (!yours) {
    return (
      <p className="type-secondary text-text-hint m-0">
        History is kept for your own skills
      </p>
    );
  }
  // A read of another version is not this version's history.
  if (read === undefined || read.version !== version) {
    return (
      <p className="type-secondary text-text-hint m-0">Reading the history…</p>
    );
  }
  if (read.failed) {
    return (
      <p className="type-secondary text-text-hint m-0">
        The history could not be read
      </p>
    );
  }
  const { entries } = read;
  if (entries.length === 0) {
    return <p className="type-secondary text-text-hint m-0">No history yet</p>;
  }
  const shown = entries.slice(0, SHOWN);
  const more = entries.length > SHOWN;
  return (
    <>
      <ol aria-label="History" className="m-0 flex list-none flex-col p-0">
        {shown.map((e, i) => (
          <Entry
            key={e.commit}
            entry={e}
            last={!more && i === shown.length - 1}
          />
        ))}
        {more && (
          <li className="grid-cols-timeline grid gap-x-3">
            <DotColumn button last>
              <svg
                viewBox="0 0 8 8"
                aria-hidden
                focusable="false"
                className="size-timeline-dot block"
              >
                <circle
                  cx="4"
                  cy="4"
                  r="3.25"
                  fill="none"
                  strokeWidth="1.5"
                  className="stroke-timeline-more-ring"
                />
              </svg>
            </DotColumn>
            <div className="flex">
              <Button
                variant="ghost"
                size="sm-wrap"
                onClick={() => {
                  setSheetOpen(true);
                }}
              >
                {`Show all ${entries.length}`}
              </Button>
            </div>
          </li>
        )}
      </ol>
      <HistorySheet
        name={row.name}
        version={version}
        entries={entries}
        open={sheetOpen}
        onOpenChange={setSheetOpen}
      />
    </>
  );
};
