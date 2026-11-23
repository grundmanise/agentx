import { useEffect, useState } from "react";

import type { HistoryEvent } from "#/cli/events.gen.ts";
import { ChevronIcon } from "#/components/icons/chevron.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "#/components/ui/sheet.tsx";
import { formatDate } from "#/lib/dates.ts";
import { openInBrowser } from "#/lib/open-url.ts";
import { isMac } from "#/lib/platform.ts";
import { useKeptRead } from "#/screens/skills/kept-read.ts";
import { shortSource } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { loadHistory } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";

/** How many entries the timeline shows before "Show all". */
const SHOWN = 3;

const files = (n: number) => (n === 1 ? "1 file" : `${n} files`);

/** What this machine is called in an entry's meta line. */
const HERE = isMac ? "this Mac" : "this machine";

/** An https URL, which the window opens; undefined for anything else. */
const https = (url: string | undefined) =>
  url?.startsWith("https://") === true ? url : undefined;

/**
 * What the meta line of an entry says: the date, the machine it was made on when known, the date
 * of the upstream version a fork or an update took in, and the number of files, except on a fork
 * or a rename, whose files are the skill's own.
 */
export const metaOf = (e: HistoryEvent, here: string | undefined) => {
  const parts = [formatDate(e.time)];
  if (e.machine !== undefined && e.machine === here) {
    parts.push(HERE);
  } else if (e.machine_label !== undefined) {
    parts.push(e.machine_label);
  }
  if (e.upstream !== undefined) {
    parts.push(`version of ${formatDate(e.upstream.time)}`);
  }
  if (e.kind !== "fork" && e.kind !== "rename" && e.files.length > 0) {
    parts.push(files(e.files.length));
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

/**
 * Whether an entry is shown: a version of the source's skill that agentx stored is not something
 * done to the skill, and the fork or update that took it in shows its date.
 */
export const isShown = (e: HistoryEvent) => e.kind !== "upstream";

/** A run of an entry's text, with the page it links to, if any. */
interface Segment {
  text: string;
  page?: string;
}

const VERBS: Record<string, string> = {
  added: "added",
  deleted: "deleted",
  modified: "edited",
};

/** What recorded edits did, as agentx's own subject says it: the first two files, then how many more. */
const editsOf = (e: HistoryEvent) => {
  const said = e.files.slice(0, 2).map((f) => `${VERBS[f.status]} ${f.path}`);
  const more = e.files.length - said.length;
  let text = said.join(", ");
  if (said.length === 0) {
    text = "edited";
  } else if (more > 0) {
    text = `${text} and ${more === 1 ? "1 more file" : `${more} more files`}`;
  }
  return `${text.charAt(0).toUpperCase()}${text.slice(1)}`;
};

/** Where a fork came from: the source as owner/repo, the plugin or the skill, with its page. */
const forkedFrom = (e: HistoryEvent): Segment[] => {
  const { origin, upstream } = e;
  if (origin === undefined) {
    return [{ text: "Forked" }];
  }
  if (origin.kind === "source") {
    const source = upstream?.source ?? origin.source ?? origin.skill;
    const page = https(upstream?.skill_url ?? origin.skill_url ?? origin.url);
    return [{ text: "Forked from " }, { page, text: shortSource(source) }];
  }
  const name =
    origin.kind === "plugin" && origin.plugin !== undefined
      ? origin.plugin
      : origin.skill;
  return [
    { text: origin.kind === "plugin" ? "Forked from plugin " : "Forked from " },
    { page: https(origin.url), text: name },
  ];
};

/**
 * An entry's text in runs, of text and of links: a short sentence for what the commit did, from the
 * kind and facts the CLI reports, or its subject for a message the user wrote, a commit made with
 * Git and a kind this app does not know. A fork's origin links to the skill's folder at the forked
 * commit, or to the origin's page, and an update's source to the folder at the version it took in.
 */
export const entrySegments = (e: HistoryEvent): Segment[] => {
  switch (e.kind) {
    case "fork": {
      return forkedFrom(e);
    }
    case "create": {
      return [{ text: "Created" }];
    }
    case "adopt": {
      return [{ text: "Imported from disk" }];
    }
    case "rename": {
      return [{ text: `Renamed from ${e.renamed_from ?? ""}`.trim() }];
    }
    case "update": {
      if (e.upstream === undefined) {
        return [{ text: "Updated from upstream" }];
      }
      return [
        { text: "Updated from " },
        {
          page: https(e.upstream.skill_url ?? e.upstream.url),
          text: shortSource(e.upstream.source),
        },
      ];
    }
    case "sync": {
      return [{ text: "Synced edits from another machine" }];
    }
    case "edit": {
      return e.generated === true
        ? [{ text: editsOf(e) }]
        : [{ text: e.subject }];
    }
    case "git":
    case "upstream": {
      return [{ text: e.subject }];
    }
    default: {
      // A kind a later CLI reports: its subject, as written.
      return [{ text: e.subject }];
    }
  }
};

/** An entry's text, plain. */
export const entryText = (e: HistoryEvent) =>
  entrySegments(e)
    .map((s) => s.text)
    .join("");

/** Whether an entry's text holds a link, which cannot sit inside its button. */
const hasLink = (e: HistoryEvent) =>
  entrySegments(e).some((s) => s.page !== undefined);

/** An entry's text, its links underlined. */
const Subject = ({ entry }: { entry: HistoryEvent }) => (
  <span className="type-timeline-entry text-text-primary min-w-0 wrap-break-word">
    {entrySegments(entry).map(({ text, page }, i) =>
      page === undefined ? (
        text
      ) : (
        <a
          // The runs of one entry never change order.
          key={i}
          href={page}
          title={page}
          className="underline underline-offset-2"
          onClick={(ev) => {
            ev.preventDefault();
            void openInBrowser(page);
          }}
        >
          {text}
        </a>
      )
    )}
  </span>
);

const Chevron = ({ open }: { open: boolean }) => (
  <span className="h-timeline-line flex flex-none items-center">
    <span className={open ? "flex rotate-90" : "flex"}>
      <ChevronIcon size="xs" />
    </span>
  </span>
);

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
  const here = useAppStore((s) => s.inventory?.machine.id);
  const linked = hasLink(entry);
  return (
    <li
      className={
        last
          ? "relative flex min-w-0 flex-col"
          : "relative flex min-w-0 flex-col pb-4"
      }
    >
      {expandable && !last && (
        // The line from this entry's chevron down to the next one's, so the sheet reads as one
        // history.
        <span
          aria-hidden
          className="bg-timeline-connector absolute top-6 bottom-1 left-1.5 w-px"
        />
      )}
      {expandable && linked && (
        // A link cannot sit inside the button, so the button holds the chevron alone.
        <div className="flex items-start gap-2">
          <Button
            variant="plain"
            size="inline"
            aria-expanded={open}
            aria-label={`Show the files of ${entryText(entry)}`}
            onClick={() => {
              setOpen((o) => !o);
            }}
          >
            <Chevron open={open} />
          </Button>
          <Subject entry={entry} />
        </div>
      )}
      {expandable && !linked && (
        // A block, so the button shrinks to fit the list and its subject wraps inside it.
        <div>
          <Button
            variant="plain"
            size="inline"
            aria-expanded={open}
            onClick={() => {
              setOpen((o) => !o);
            }}
          >
            <Chevron open={open} />
            <Subject entry={entry} />
          </Button>
        </div>
      )}
      {!expandable && <Subject entry={entry} />}
      {/* The meta line and the files sit under the text, past the chevron, which hangs outside. */}
      <div
        className={
          expandable
            ? "pl-timeline-indent flex min-w-0 flex-col"
            : "flex min-w-0 flex-col"
        }
      >
        <span className="type-timeline-meta text-timeline-meta">
          {metaOf(entry, here)}
        </span>
        {expandable && open && (
          <ul
            aria-label={`Files of ${entryText(entry)}`}
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
  <Sheet open={open} onOpenChange={onOpenChange}>
    <SheetContent name="history">
      <SheetHeader>
        <SheetTitle>History</SheetTitle>
        <SheetDescription>{name}</SheetDescription>
      </SheetHeader>
      {/* The entries scroll down only; a long entry wraps. */}
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
            />
          ))}
        </ol>
      </div>
    </SheetContent>
  </Sheet>
);

/**
 * HISTORY in the detail pane, for one of your own skills: the latest commits of its branch from
 * `skill history <name>`, read when the skill is selected, each said as what it did to the skill;
 * "Show all" opens the sheet. Other skills have no history of their own, and the pane shows no
 * section for them.
 */
export const HistorySection = ({ row }: { row: SkillRow }) => {
  const version = versionOf(row);
  const stored = useAppStore((s) => s.histories[row.name]);
  // A read of another version is not this version's history; the one shown before stays meanwhile.
  const read = useKeptRead(stored?.version === version ? stored : undefined);
  const [sheetOpen, setSheetOpen] = useState(false);

  useEffect(() => {
    void loadHistory(row.name, version);
  }, [row.name, version]);

  if (read === undefined) {
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
  const entries = read.entries.filter(isShown);
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
          <li className="-ml-3.5 flex">
            <Button
              variant="ghost"
              size="sm-wrap"
              onClick={() => {
                setSheetOpen(true);
              }}
            >
              {`Show all ${entries.length}`}
            </Button>
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
