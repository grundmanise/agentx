// The machine the Settings tests of the account remote and the sources run against: an account
// remote with three of your skills, fetched two hours ago, that this machine can write to; a shared
// source pinned to v2, fetched three days ago, read-only here, whose listing holds four skills; and
// a shared source never fetched, whose access is unknown. Inline events with only the fields the
// tests check, typed by the generated event types.
import type { SourceEvent, SourceSkillEvent } from "#/cli/events.gen.ts";

export const ACCOUNT = "ssh://git@github.com/me/account";
export const ACCOUNT_ID = "5e8a1c3b7d9f2a64";
export const ACME = "https://github.com/acme/skills";
export const ACME_ID = "1c9d0b7e4a2f6d38";
export const ZETA = "https://gitlab.com/zeta/tools";
export const ZETA_ID = "7a3b5c9e1f2d4a60";

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

export const acme: SourceEvent = {
  access: "read-only",
  commit: "3f2a9c1e8d7b6a",
  id: ACME_ID,
  last_fetched: ago(3 * DAY + 2 * HOUR),
  pin: "v2",
  schema_version: 2,
  type: "source",
  url: ACME,
};

export const zeta: SourceEvent = {
  id: ZETA_ID,
  schema_version: 2,
  type: "source",
  url: ZETA,
};

/** What `source list` reports: the account remote and both shared sources. */
export const sources: SourceEvent[] = [acme, zeta, account];

/** What `source list` reports on a machine with no account remote. */
export const sharedOnly: SourceEvent[] = [acme, zeta];

const skill = (name: string): SourceSkillEvent => ({
  description: `The ${name} skill`,
  name,
  schema_version: 2,
  source: ACME_ID,
  subpath: `skills/${name}`,
  tree: `t-${name}`,
  type: "source_skill",
});

/** acme's listing: four skills. */
export const acmeSkills: SourceSkillEvent[] = [
  skill("artifacts"),
  skill("docx"),
  skill("forms"),
  skill("letters"),
];
