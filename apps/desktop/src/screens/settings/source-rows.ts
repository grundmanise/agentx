// What the Account remote section of Settings shows, projected from the Sources slice
// (`source list`). Pure functions, so the section and the tests read the account remote the same
// way.
import type { SourceAccess, SourceEvent } from "#/cli/events.gen.ts";
import { formatAgo } from "#/lib/dates.ts";

/** The account remote: the one entry `source add --account` wrote, if any. */
export const accountRemote = (
  sources: SourceEvent[]
): SourceEvent | undefined => sources.find((s) => s.account === true);

/** What this machine may do at a source, as the last access check found. */
export const accessLine = (access: SourceAccess | undefined): string => {
  if (access === "writable") {
    return "You can write to it";
  }
  return access === "read-only" ? "Read-only for you" : "Write access unknown";
};

/** "2 of your skills on it · fetched 2 hours ago", or "Not fetched yet". */
export const accountLine = (account: SourceEvent, now: number): string => {
  const ago =
    account.last_fetched === undefined
      ? undefined
      : formatAgo(account.last_fetched, now);
  if (ago === undefined) {
    return "Not fetched yet";
  }
  return account.skills === undefined
    ? `Fetched ${ago}`
    : `${account.skills} of your skills on it · fetched ${ago}`;
};
