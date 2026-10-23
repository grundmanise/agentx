import { useEffect, useState } from "react";

import { Spinner } from "#/components/spinner.tsx";
import { CHECK_DEADLINE_MS } from "#/startup/startup.ts";
import type { Step } from "#/startup/startup.ts";

/** What serve's first scan looks for, named in turn while it runs. */
const looking = [
  "Looking for skills",
  "Looking for MCP servers",
  "Looking for plugins",
];
const LOOK_MS = 1500;
/** From this far into a check, the panel says when the check times out. */
const COUNTDOWN_AFTER_MS = 5000;

/** When the caller mounted, and the time now, read again every half second while mounted. */
const useClock = () => {
  const [clock, setClock] = useState(() => {
    const at = Date.now();
    return { now: at, shownAt: at };
  });
  useEffect(() => {
    const timer = setInterval(() => {
      setClock((c) => ({ ...c, now: Date.now() }));
    }, 500);
    return () => {
      clearInterval(timer);
    };
  }, []);
  return clock;
};

/**
 * What a launch that runs over 2 seconds shows in the empty panel: what the checks do now and,
 * from 5 seconds into a check, when that check times out. A screen reader hears each step once,
 * not each turn of the text or each second of the countdown.
 */
export const LaunchProgress = ({ step }: { step: Step }) => {
  const { now, shownAt } = useClock();
  const elapsed = Math.max(0, now - step.startedAt);
  const machine = step.kind === "machine";
  // The names turn from when they first show, so skills always come first.
  const turns = Math.floor(
    Math.max(0, now - Math.max(shownAt, step.startedAt)) / LOOK_MS
  );
  const text = machine
    ? "Checking this machine"
    : looking[turns % looking.length];
  const late = elapsed >= COUNTDOWN_AFTER_MS;
  const left = Math.max(0, Math.ceil((CHECK_DEADLINE_MS - elapsed) / 1000));
  return (
    <output className="animate-fade-in flex flex-1 flex-col items-center justify-center gap-1">
      <span className="sr-only">
        {machine
          ? "Checking this machine"
          : "Looking for skills, MCP servers and plugins"}
        {late && ". Taking longer than usual"}
      </span>
      <span
        aria-hidden
        className="type-secondary text-text-muted flex items-center gap-2"
      >
        <Spinner />
        <span key={text} className="animate-fade-in">
          {text}
        </span>
      </span>
      {late && (
        <span aria-hidden className="type-row-desc text-text-hint">
          Taking longer than usual · times out in {left}s
        </span>
      )}
    </output>
  );
};
