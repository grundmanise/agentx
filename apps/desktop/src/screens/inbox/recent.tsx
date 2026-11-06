// RECENT, the right column of the Inbox (DESIGN.md §6 "Recent"): what agentx and the user just
// did, from the store's activity list, newest first. Each entry is its time, a dot in the tone of
// the outcome and the message. New entries come in live as the list changes.
import { cva } from "class-variance-authority";

import { useAppStore } from "#/store/store.ts";
import type { ActivityEntry } from "#/store/store.ts";

/** How many entries the column shows. */
export const RECENT_LIMIT = 9;

const dotVariants = cva("size-recent-dot rounded-pill block flex-none", {
  variants: {
    tone: {
      attention: "bg-tone-attention-fg",
      danger: "bg-tone-danger-fg",
      success: "bg-tone-primary-fg",
    },
  },
});

/** The entry's time as HH:MM, in the machine's local time. */
const timeOf = (at: number) =>
  new Date(at).toLocaleTimeString(undefined, {
    hour: "2-digit",
    hourCycle: "h23",
    minute: "2-digit",
  });

const Entry = ({ entry }: { entry: ActivityEntry }) => (
  <li data-tone={entry.tone} className="flex items-start gap-3">
    <time
      dateTime={new Date(entry.at).toISOString()}
      className="type-technical-sm text-text-caption w-recent-time flex-none pt-0.5"
    >
      {timeOf(entry.at)}
    </time>
    <span className="flex h-5 flex-none items-center">
      <span className={dotVariants({ tone: entry.tone })} />
    </span>
    <span className="type-secondary text-text-secondary min-w-0 flex-1 text-pretty">
      {entry.message}
    </span>
  </li>
);

export const Recent = () => {
  const activity = useAppStore((s) => s.activity);
  const shown = activity.slice(0, RECENT_LIMIT);
  return (
    <section aria-label="Recent" className="flex flex-col gap-4">
      <h2 className="type-caption text-text-caption m-0">Recent</h2>
      {shown.length === 0 ? (
        <p className="type-secondary text-text-hint m-0">Nothing yet</p>
      ) : (
        <ol className="m-0 flex list-none flex-col gap-3 p-0">
          {shown.map((entry) => (
            <Entry key={entry.id} entry={entry} />
          ))}
        </ol>
      )}
    </section>
  );
};
