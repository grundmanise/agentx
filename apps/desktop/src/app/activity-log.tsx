// The activity log's sheet (DESIGN.md §6 "Sheet (Activity log)"): every command the app ran this
// session and what the serve child reported, newest first, from src/store/activity-log.ts. An
// entry opens to its output and copies as plain text. Settings and the palette open it.
import { cva } from "class-variance-authority";
import { useEffect, useId, useState } from "react";

import { ChevronIcon } from "#/components/icons/chevron.tsx";
import { notify } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "#/components/ui/sheet.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { revealInFinder, showInLabel } from "#/lib/reveal.ts";
import {
  entryText,
  formatDuration,
  openActivityLog,
  setDebugLines,
  statusOf,
  useActivityLog,
} from "#/store/activity-log.ts";
import type { LogEntry, Output } from "#/store/activity-log.ts";
import { readServeLog, setServeDebug } from "#/store/feed.ts";

const dotVariants = cva("size-log-dot rounded-pill block flex-none", {
  variants: {
    tone: {
      attention: "bg-log-dot-attention",
      danger: "bg-log-dot-danger",
      neutral: "bg-log-dot-neutral",
      success: "bg-log-dot-success",
    },
  },
});

const statusVariants = cva("", {
  variants: {
    tone: {
      attention: "text-tone-attention-fg",
      danger: "text-tone-danger-fg",
      neutral: "text-text-caption",
      success: "text-text-caption",
    },
  },
});

/** The time of day an entry started, to the second. */
const timeOf = (at: number) =>
  new Date(at).toLocaleTimeString(undefined, {
    hour: "2-digit",
    hour12: false,
    minute: "2-digit",
    second: "2-digit",
  });

const copy = async (text: string, toast: string) => {
  try {
    await navigator.clipboard.writeText(text);
    notify(toast);
  } catch {
    notify("Couldn't copy to the clipboard");
  }
};

/** One stream of a command's output, or a serve report's lines, under its caption. */
const Lines = ({ caption, output }: { caption: string; output: Output }) => {
  const id = useId();
  if (output.lines.length === 0) {
    return null;
  }
  return (
    <div className="flex flex-col gap-1.5">
      <span id={id} className="type-caption text-text-caption">
        {caption}
      </span>
      <pre
        aria-labelledby={id}
        className="rounded-log-output border-code-border bg-code-bg type-technical-sm text-text-secondary max-h-log-output m-0 overflow-auto border px-3 py-2.5 break-all whitespace-pre-wrap"
      >
        {output.lines.join("\n")}
        {output.dropped > 0 && `\n… ${output.dropped} more lines`}
      </pre>
    </div>
  );
};

/** What an open entry shows: why it could not start, its output, and Copy. */
const Details = ({ entry }: { entry: LogEntry }) => {
  const ending = entry.kind === "command" ? entry.ending : undefined;
  const empty =
    ending !== undefined &&
    ending.startError === undefined &&
    ending.events.lines.length === 0 &&
    ending.stderr.lines.length === 0;
  return (
    <div className="flex flex-col gap-3 pt-1 pb-3 pl-3.5">
      {ending?.startError !== undefined && (
        <p className="type-notice text-tone-danger-fg m-0 break-all">
          {ending.startError}
        </p>
      )}
      {entry.kind === "command" && ending === undefined && (
        <p className="type-secondary text-text-hint m-0">Still running</p>
      )}
      {empty && <p className="type-secondary text-text-hint m-0">No output</p>}
      {ending !== undefined && (
        <>
          <Lines caption="Events" output={ending.events} />
          <Lines caption="Stderr" output={ending.stderr} />
        </>
      )}
      {entry.kind === "serve" && (
        <Lines
          caption="Reported"
          output={{ dropped: 0, lines: entry.details }}
        />
      )}
      <div className="flex">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            void copy(entryText(entry), "Copied the entry");
          }}
        >
          Copy
        </Button>
      </div>
    </div>
  );
};

const Entry = ({ entry }: { entry: LogEntry }) => {
  const [open, setOpen] = useState(false);
  const status = statusOf(entry);
  const took =
    entry.kind === "command" && entry.ending !== undefined
      ? formatDuration(entry.ending.ms)
      : undefined;
  return (
    <li className="flex flex-col">
      <Button
        variant="ghost"
        size="sm-wrap"
        aria-expanded={open}
        onClick={() => {
          setOpen((o) => !o);
        }}
      >
        <span className="flex h-5 flex-none items-center gap-2.5">
          <span className={open ? "flex rotate-90" : "flex"}>
            <ChevronIcon size="xs" />
          </span>
          <span className={dotVariants({ tone: status.tone })} />
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          {entry.kind === "command" ? (
            <span className="type-technical text-text-primary break-all">
              {entry.line}
            </span>
          ) : (
            <span className="type-secondary text-text-primary">
              {entry.title}
            </span>
          )}
          <span className="type-technical-sm text-text-caption">
            {timeOf(entry.at)}
            {took !== undefined && ` · ${took}`}
            {" · "}
            <span className={statusVariants({ tone: status.tone })}>
              {status.text}
            </span>
          </span>
        </span>
      </Button>
      {open && <Details entry={entry} />}
    </li>
  );
};

type ServeLogRead =
  | { kind: "reading" }
  | { kind: "failed" }
  | { kind: "read"; path: string; lines: string[] };

/** The end of the serve log file, read when this mounts: its path, its lines, Copy and Show in Finder. */
const ServeLogDetails = () => {
  const [read, setRead] = useState<ServeLogRead>({ kind: "reading" });
  useEffect(() => {
    let current = true;
    const load = async () => {
      const log = await readServeLog();
      if (current) {
        setRead(
          log === undefined ? { kind: "failed" } : { kind: "read", ...log }
        );
      }
    };
    void load();
    return () => {
      current = false;
    };
  }, []);
  if (read.kind !== "read") {
    return (
      <p className="type-secondary text-text-hint m-0 pt-1 pb-3 pl-3.5">
        {read.kind === "reading"
          ? "Reading the serve log…"
          : "The serve log could not be read"}
      </p>
    );
  }
  const { path, lines } = read;
  return (
    <div className="flex flex-col gap-3 pt-1 pb-3 pl-3.5">
      <span className="type-technical-sm text-text-caption break-all">
        {path}
      </span>
      {lines.length === 0 ? (
        <p className="type-secondary text-text-hint m-0">
          The serve log is empty
        </p>
      ) : (
        <Lines caption="Last lines" output={{ dropped: 0, lines }} />
      )}
      <div className="gap-button flex">
        {lines.length > 0 && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              void copy(lines.join("\n"), "Copied the serve log");
            }}
          >
            Copy
          </Button>
        )}
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            void revealInFinder(path);
          }}
        >
          {showInLabel()}
        </Button>
      </div>
    </div>
  );
};

/**
 * The serve log file, above the entries: everything `agentx serve` wrote to stderr, this session
 * and the ones before it. It is read each time it is opened.
 */
const ServeLogFile = () => {
  const [open, setOpen] = useState(false);
  return (
    <div className="flex flex-col">
      <Button
        variant="ghost"
        size="sm-wrap"
        aria-expanded={open}
        onClick={() => {
          setOpen((o) => !o);
        }}
      >
        <span className="flex h-5 flex-none items-center">
          <span className={open ? "flex rotate-90" : "flex"}>
            <ChevronIcon size="xs" />
          </span>
        </span>
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="type-secondary text-text-primary">Serve log</span>
          <span className="type-technical-sm text-text-caption">
            What agentx serve wrote to its log file
          </span>
        </span>
      </Button>
      {open && <ServeLogDetails />}
    </div>
  );
};

/** The switch that makes the commands run from now on, and serve, log at debug level. */
const DebugLines = () => {
  const id = useId();
  const debug = useActivityLog((s) => s.debug);
  return (
    <div className="flex items-center gap-4">
      <label htmlFor={id} className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="type-body text-text-primary">Debug lines</span>
        <span id={`${id}-line`} className="type-secondary text-text-muted">
          Commands that run from now on record what agentx logs at debug level,
          and agentx serve restarts to write it to the serve log
        </span>
      </label>
      <Switch
        id={id}
        aria-describedby={`${id}-line`}
        checked={debug}
        onCheckedChange={(on) => {
          setDebugLines(on);
          void setServeDebug(on);
        }}
      />
    </div>
  );
};

/** The activity log, in the sheet at the right edge. */
export const ActivityLogSheet = () => {
  const open = useActivityLog((s) => s.open);
  const entries = useActivityLog((s) => s.entries);
  return (
    <Sheet open={open} onOpenChange={openActivityLog}>
      <SheetContent size="wide">
        <SheetHeader>
          <SheetTitle>Activity log</SheetTitle>
          <SheetDescription>This session, newest first</SheetDescription>
        </SheetHeader>
        <DebugLines />
        <div className="flex min-h-0 flex-1 flex-col gap-1 overflow-auto">
          <ServeLogFile />
          {entries.length === 0 ? (
            <p className="type-secondary text-text-hint m-0">Nothing ran yet</p>
          ) : (
            <ol
              aria-label="Activity log entries"
              className="m-0 flex list-none flex-col gap-1 p-0"
            >
              {entries.map((entry) => (
                <Entry key={entry.id} entry={entry} />
              ))}
            </ol>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
};
