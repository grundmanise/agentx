// The activity log's sheet (DESIGN.md §6 "Sheet (Activity log)"): the last lines of what the app
// ran and what the background scan reported, as a terminal shows them, from
// src/store/activity-log.ts. A failure is red. Settings and the palette open it.
import { cva } from "class-variance-authority";
import { useId, useLayoutEffect, useRef } from "react";

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
import { plural } from "#/lib/plural.ts";
import { revealInFinder } from "#/lib/reveal.ts";
import {
  LINE_LIMIT,
  logText,
  openActivityLog,
  setDebugOutput,
  useActivityLog,
} from "#/store/activity-log.ts";
import type { LogLine } from "#/store/activity-log.ts";
import { serveLogPath, setServeDebug } from "#/store/feed.ts";

/** How near its end, in pixels, the view still counts as at the end. */
const END_SLACK = 24;

const lineVariants = cva("type-technical-sm flex gap-3", {
  variants: {
    tone: {
      failed: "text-tone-danger-fg",
      plain: "text-text-secondary",
      quiet: "text-text-caption",
    },
  },
});

/** The time and the note of a line: muted on a plain line, the line's own colour otherwise. */
const asideVariants = cva("flex-none", {
  variants: {
    part: {
      note: "",
      // As wide as a time on a line of output, which has none, so the text lines up.
      time: "w-log-time",
    },
    tone: {
      failed: "",
      plain: "text-text-caption",
      quiet: "",
    },
  },
});

/** The time of day a line was recorded, to the second. */
const timeOf = (at: number) =>
  new Date(at).toLocaleTimeString(undefined, {
    hour: "2-digit",
    hour12: false,
    minute: "2-digit",
    second: "2-digit",
  });

const count = (n: number) => n.toLocaleString("en-US");

const copy = async (text: string) => {
  try {
    await navigator.clipboard.writeText(text);
    notify("Copied");
  } catch {
    notify("Couldn't copy to the clipboard");
  }
};

/** Shows the serve log file in the file manager. */
const showServeLog = async () => {
  const path = await serveLogPath();
  if (path === undefined) {
    notify("Couldn't find the serve log");
    return;
  }
  await revealInFinder(path);
};

const Line = ({ line }: { line: LogLine }) => (
  <li className={lineVariants({ tone: line.tone })}>
    <span className={asideVariants({ part: "time", tone: line.tone })}>
      {line.at !== undefined && timeOf(line.at)}
    </span>
    <span className="min-w-0 flex-1 break-all whitespace-pre-wrap">
      {line.text}
    </span>
    {line.note !== "" && (
      <span className={asideVariants({ part: "note", tone: line.tone })}>
        {line.note}
      </span>
    )}
  </li>
);

/**
 * The lines, oldest first. The view stays at the end as lines arrive while it is at the end; a
 * view scrolled up stays where it is.
 */
const Stream = ({ lines }: { lines: LogLine[] }) => {
  const list = useRef<HTMLOListElement>(null);
  const atEnd = useRef(true);
  // After every render, which new lines cause.
  useLayoutEffect(() => {
    const el = list.current;
    if (el !== null && atEnd.current) {
      el.scrollTop = el.scrollHeight;
    }
  });
  return (
    <ol
      ref={list}
      aria-label="Activity log lines"
      className="rounded-log-output border-code-border bg-code-bg m-0 flex min-h-0 flex-1 list-none flex-col gap-0.5 overflow-auto border px-3 py-2.5"
      onScroll={({ currentTarget: el }) => {
        atEnd.current =
          el.scrollHeight - el.scrollTop - el.clientHeight < END_SLACK;
      }}
    >
      {lines.map((line) => (
        <Line key={line.id} line={line} />
      ))}
    </ol>
  );
};

/** The switch that makes the commands run from now on, and serve, log at debug level. */
const DebugOutput = () => {
  const id = useId();
  const debug = useActivityLog((s) => s.debug);
  return (
    <div className="flex items-center gap-4">
      <label htmlFor={id} className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="type-body text-text-primary">Debug output</span>
        <span id={`${id}-line`} className="type-secondary text-text-muted">
          Adds debug lines to new commands and to the serve log. Restarts the
          background scan.
        </span>
      </label>
      <Switch
        id={id}
        aria-describedby={`${id}-line`}
        checked={debug}
        onCheckedChange={(on) => {
          setDebugOutput(on);
          void setServeDebug(on);
        }}
      />
    </div>
  );
};

/** Under the lines: how many the log keeps and how many it dropped, then how many failed. */
const Counts = ({ lines, dropped }: { lines: LogLine[]; dropped: number }) => {
  const failed = lines.filter(
    (line) => line.tone === "failed" && line.at !== undefined
  ).length;
  return (
    <div className="type-technical-sm text-text-caption flex justify-between gap-3">
      <span>
        {count(lines.length)} of {count(LINE_LIMIT)} lines kept ·{" "}
        {count(dropped)} dropped
      </span>
      <span className={failed === 0 ? "" : "text-tone-danger-fg"}>
        {count(failed)} failed
      </span>
    </div>
  );
};

/** The activity log, in the sheet at the right edge. */
export const ActivityLogSheet = () => {
  const open = useActivityLog((s) => s.open);
  const lines = useActivityLog((s) => s.lines);
  const dropped = useActivityLog((s) => s.dropped);
  return (
    <Sheet open={open} onOpenChange={openActivityLog}>
      <SheetContent size="wide">
        <SheetHeader>
          <SheetTitle>Activity log</SheetTitle>
          <SheetDescription>
            The last {plural(LINE_LIMIT, "line")}
          </SheetDescription>
        </SheetHeader>
        <DebugOutput />
        <div className="flex min-h-0 flex-1 flex-col gap-3">
          <div className="gap-button flex">
            <Button
              variant="ghost"
              size="sm"
              disabled={lines.length === 0}
              onClick={() => {
                void copy(logText(lines));
              }}
            >
              Copy
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => {
                void showServeLog();
              }}
            >
              Open serve.log
            </Button>
          </div>
          {lines.length === 0 ? (
            <p className="type-secondary text-text-hint m-0 flex-1">
              No commands yet
            </p>
          ) : (
            <Stream lines={lines} />
          )}
          <Counts lines={lines} dropped={dropped} />
        </div>
      </SheetContent>
    </Sheet>
  );
};
