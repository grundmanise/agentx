import { useEffect, useState } from "react";

import type { ForkOrigin, HistoryEvent } from "#/cli/events.gen.ts";
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
import { useKeptRead } from "#/screens/skills/kept-read.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { loadHistory } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";

/** How many entries the timeline shows before "Show all". */
const SHOWN = 3;

const files = (n: number) => (n === 1 ? "1 file" : `${n} files`);

/** What the meta line of an entry says: the date and the number of files. */
const metaOf = (e: HistoryEvent) =>
  [formatDate(e.time), files(e.files.length)].join(" · ");

/** The version a skill's history is read for: a new read when the skill changed. */
const versionOf = (row: SkillRow) =>
  [
    row.entry?.content_hash ?? "",
    row.entry?.state ?? "",
    ...(row.entry?.drift ?? []),
  ].join(":");

/**
 * The page of the skill a fork came from, on the commit that created the fork, when the CLI knows
 * one: an https URL alone, since the window opens nothing else.
 */
export const originPage = (e: HistoryEvent) =>
  e.origin?.url?.startsWith("https://") === true ? e.origin.url : undefined;

/**
 * Text as the CLI writes it into a commit's subject: a control character as a space, runs of
 * spaces as one, none at either end.
 */
const displayed = (text: string) =>
  text
    .replaceAll(/\p{Cc}/gu, " ")
    .replaceAll(/ +/gu, " ")
    .trim();

const originName = (origin: ForkOrigin) => {
  if (origin.kind === "plugin" && origin.plugin !== undefined) {
    return origin.plugin;
  }
  if (origin.kind === "source" && origin.source !== undefined) {
    return origin.path === undefined || origin.path === ""
      ? origin.source
      : `${origin.source}/${origin.path}`;
  }
  return origin.skill;
};

/**
 * The subject of the commit that created a fork, split around the name of its origin, the part
 * that links to the origin's page: the plugin, the source with its path, or the skill, as the CLI
 * writes them after "from". Undefined when the subject does not name it.
 */
export const originParts = (e: HistoryEvent) => {
  const name = e.origin === undefined ? "" : displayed(originName(e.origin));
  const from = e.subject.indexOf(" from ");
  const at =
    name === "" || from === -1
      ? -1
      : e.subject.indexOf(name, from + " from ".length);
  return at === -1
    ? undefined
    : {
        after: e.subject.slice(at + name.length),
        before: e.subject.slice(0, at),
        name,
      };
};

/** An https URL, which the window opens; undefined for anything else. */
const https = (url: string | undefined) =>
  url?.startsWith("https://") === true ? url : undefined;

/** A run of an entry's subject, with the page it links to, if any. */
interface Segment {
  text: string;
  page?: string;
}

/**
 * An entry's subject in runs, of text and of links. On the commit that created a fork, the
 * origin's name links to its page, and for a fork of a source's skill, the fork's name, after
 * "Fork", links to the page of the skill's directory at the commit it was forked from, each when
 * the CLI knows it.
 */
export const subjectSegments = (e: HistoryEvent): Segment[] => {
  const links: { at: number; end: number; page: string }[] = [];
  const skillPage =
    e.origin?.kind === "source" ? https(e.origin.skill_url) : undefined;
  const from = e.subject.indexOf(" from ");
  if (
    skillPage !== undefined &&
    e.subject.startsWith("Fork ") &&
    from > "Fork ".length
  ) {
    links.push({ at: "Fork ".length, end: from, page: skillPage });
  }
  const page = originPage(e);
  const parts = page === undefined ? undefined : originParts(e);
  if (page !== undefined && parts !== undefined) {
    const at = parts.before.length;
    links.push({ at, end: at + parts.name.length, page });
  }
  const segments: Segment[] = [];
  let done = 0;
  for (const link of links) {
    if (link.at < done) {
      continue;
    }
    if (link.at > done) {
      segments.push({ text: e.subject.slice(done, link.at) });
    }
    segments.push({
      page: link.page,
      text: e.subject.slice(link.at, link.end),
    });
    done = link.end;
  }
  if (done < e.subject.length) {
    segments.push({ text: e.subject.slice(done) });
  }
  return segments;
};

/** Whether an entry's subject holds a link, which cannot sit inside its button. */
const hasLink = (e: HistoryEvent) =>
  subjectSegments(e).some((s) => s.page !== undefined);

/** An entry's subject, its links underlined. */
const Subject = ({ entry }: { entry: HistoryEvent }) => (
  <span className="type-timeline-entry text-text-primary min-w-0 wrap-break-word">
    {subjectSegments(entry).map(({ text, page }, i) =>
      page === undefined ? (
        text
      ) : (
        <a
          // The runs of one subject never change order.
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
            aria-label={`Show the files of ${entry.subject}`}
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
      {/* The meta line and the files sit under the subject, past the chevron, which hangs outside. */}
      <div
        className={
          expandable
            ? "pl-timeline-indent flex min-w-0 flex-col"
            : "flex min-w-0 flex-col"
        }
      >
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
  <Sheet open={open} onOpenChange={onOpenChange}>
    <SheetContent name="history">
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
            />
          ))}
        </ol>
      </div>
    </SheetContent>
  </Sheet>
);

/**
 * HISTORY in the detail pane. For one of your own skills, the latest commits of its branch from
 * `skill history <name>`, read when the skill is selected; "Show all" opens the sheet. In the
 * subject of the commit that created a fork, the origin's name links to its page, when the CLI
 * knows one. Other
 * skills have no history of their own.
 */
export const HistorySection = ({ row }: { row: SkillRow }) => {
  const yours = row.kind === "yours";
  const version = versionOf(row);
  const stored = useAppStore((s) => s.histories[row.name]);
  // A read of another version is not this version's history; the one shown before stays meanwhile.
  const read = useKeptRead(stored?.version === version ? stored : undefined);
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
