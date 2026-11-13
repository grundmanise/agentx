// The machine the Settings tests of the account remote run against: an account remote with three
// of your skills, fetched two hours ago, that this machine can write to; a shared source pinned to
// v2, fetched three days ago, read-only here; and a shared source never fetched, whose access is
// unknown. Inline events with only the fields the tests check, typed by the generated event types.
import type { SourceEvent } from "#/cli/events.gen.ts";

export const ACCOUNT = "ssh://git@github.com/me/account";
const ACCOUNT_ID = "5e8a1c3b7d9f2a64";
export const ACME = "https://github.com/acme/skills";
const ACME_ID = "1c9d0b7e4a2f6d38";
const ZETA = "https://gitlab.com/zeta/tools";
const ZETA_ID = "7a3b5c9e1f2d4a60";

const HOUR = 3_600_000;
const DAY = 24 * HOUR;
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

export const account: SourceEvent = {
  access: "writable",
  account: true,
  id: ACCOUNT_ID,
  last_fetched: ago(2 * HOUR),
  schema_version: 2,
  skills: 3,
  type: "source",
  url: ACCOUNT,
};

const acme: SourceEvent = {
  access: "read-only",
  commit: "3f2a9c1e8d7b6a",
  id: ACME_ID,
  last_fetched: ago(3 * DAY + 2 * HOUR),
  pin: "v2",
  schema_version: 2,
  type: "source",
  url: ACME,
};

const zeta: SourceEvent = {
  id: ZETA_ID,
  schema_version: 2,
  type: "source",
  url: ZETA,
};

/** What `source list` reports: the account remote and both shared sources. */
export const sources: SourceEvent[] = [acme, zeta, account];

/** What `source list` reports on a machine with no account remote. */
export const sharedOnly: SourceEvent[] = [acme, zeta];
