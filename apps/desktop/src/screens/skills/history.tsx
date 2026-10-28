import { cva } from "class-variance-authority";
import { useEffect, useState } from "react";

import type { HistoryEvent } from "#/cli/events.gen.ts";
import { ChevronIcon } from "#/components/icons/chevron.tsx";
import { Sheet } from "#/components/sheet.tsx";
import { Button } from "#/components/ui/button.tsx";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { loadHistory } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";

/** How many entries the timeline shows before "Show all". */
const SHOWN = 3;

const dotVariants = cva("size-timeline-dot rounded-pill mt-1.5 block", {
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

/** The author date of a commit: "Oct 2", with the year when it is not this year's. */
export const formatDate = (time: string) => {
  const date = new Date(time);
  if (Number.isNaN(date.getTime())) {
    return time;
  }
  const thisYear = date.getFullYear() === new Date().getFullYear();
  return date.toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    ...(thisYear ? {} : { year: "numeric" }),
  });
};

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

const Entry = ({
  entry,
  last,
  expandable = false,
}: {
  entry: HistoryEvent;
  last: boolean;
  /** Shows a chevron that opens the files the commit changed. */
  expandable?: boolean;
}) => {
  const [open, setOpen] = useState(false);
  return (
    <li className="grid-cols-timeline grid gap-x-3">
      <span className="flex flex-col items-center">
        <span className={dotVariants({ kind: kindOf(entry) })} />
        {!last && <span className="bg-timeline-connector mt-1.5 w-px flex-1" />}
      </span>
      <div
        className={
          last ? "flex min-w-0 flex-col" : "flex min-w-0 flex-col pb-4"
        }
      >
        {expandable ? (
          <div className="-ml-3.5 flex">
            <Button
              variant="ghost"
              size="sm"
              aria-expanded={open}
              onClick={() => {
                setOpen((o) => !o);
              }}
            >
              <span className={open ? "flex rotate-90" : "flex"}>
                <ChevronIcon size="xs" />
              </span>
              <span className="type-timeline-entry text-text-primary truncate">
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
      </div>
    </li>
  );
};

/** The History sheet: every commit of the skill, each expandable to the files it changed. */
const HistorySheet = ({
  name,
  entries,
  open,
  onOpenChange,
}: {
  name: string;
  entries: HistoryEvent[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => (
  <Sheet
    open={open}
    onOpenChange={onOpenChange}
    title="History"
    subtitle={name}
  >
    <ol aria-label="All history" className="m-0 flex list-none flex-col p-0">
      {entries.map((e, i) => (
        <Entry
          key={e.commit}
          entry={e}
          last={i === entries.length - 1}
          expandable
        />
      ))}
    </ol>
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
  const entries = useAppStore((s) => s.histories[row.name]);
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
  if (entries === undefined) {
    return (
      <p className="type-secondary text-text-hint m-0">Reading the history…</p>
    );
  }
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
            <span className="flex flex-col items-center">
              <svg
                viewBox="0 0 8 8"
                aria-hidden
                focusable="false"
                className="size-timeline-dot mt-1.5 block"
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
            </span>
            <div className="-mt-1 flex">
              <Button
                variant="ghost"
                size="sm"
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
        entries={entries}
        open={sheetOpen}
        onOpenChange={setSheetOpen}
      />
    </>
  );
};
