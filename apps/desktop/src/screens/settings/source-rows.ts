// What the Account remote and Sources sections of Settings show, projected from the Sources slice
// (`source list`) and the listings read so far (`source skills <id>`), and the command lines their
// controls run. Pure functions, so the sections and the tests read a source the same way.
import type { SourceAccess, SourceEvent } from "#/cli/events.gen.ts";
import { formatAgo } from "#/lib/dates.ts";
import { plural } from "#/lib/plural.ts";

/** The account remote: the one entry `source add --account` wrote, if any. */
export const accountRemote = (
  sources: SourceEvent[]
): SourceEvent | undefined => sources.find((s) => s.account === true);

/** The shared sources, in the order `source list` gives them. */
export const sharedSources = (sources: SourceEvent[]): SourceEvent[] =>
  sources.filter((s) => s.account !== true);

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

/**
 * "Fetched 3 days ago · 4 skills", or "Not fetched yet". `skills` is how many skills the source's
 * listing holds, undefined until it is read.
 */
export const fetchedLine = (
  source: SourceEvent,
  skills: number | undefined,
  now: number
): string => {
  const ago =
    source.last_fetched === undefined
      ? undefined
      : formatAgo(source.last_fetched, now);
  if (ago === undefined) {
    return "Not fetched yet";
  }
  return skills === undefined
    ? `Fetched ${ago}`
    : `Fetched ${ago} · ${plural(skills, "skill")}`;
};

/** The argument of `source add` that adds the source again as it was: its URL, and `#<pin>`. */
export const addAgainArgument = (source: SourceEvent): string =>
  source.pin === undefined ? source.url : `${source.url}#${source.pin}`;
