import { plural } from "#/lib/plural.ts";

/** A date the CLI reports (RFC 3339) as "Oct 2", with the year when it is not this year's. */
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

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/**
 * How long ago `time`, a date the CLI reports (RFC 3339), was at `now` (Date.now()): "just now",
 * "5 minutes ago", "2 hours ago", "3 days ago". Undefined when the CLI's date does not parse.
 */
export const formatAgo = (time: string, now: number): string | undefined => {
  const at = Date.parse(time);
  if (Number.isNaN(at)) {
    return undefined;
  }
  const age = Math.max(0, now - at);
  if (age < MINUTE) {
    return "just now";
  }
  if (age < HOUR) {
    return `${plural(Math.floor(age / MINUTE), "minute")} ago`;
  }
  if (age < DAY) {
    return `${plural(Math.floor(age / HOUR), "hour")} ago`;
  }
  return `${plural(Math.floor(age / DAY), "day")} ago`;
};
