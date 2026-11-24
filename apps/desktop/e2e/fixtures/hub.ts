// The machine the Settings tests of the hub run against: a hub with three
// hub skills, fetched two hours ago, that this machine can write to; a shared source pinned to
// v2, fetched three days ago, read-only here; and a shared source never fetched, whose access is
// unknown. Inline events with only the fields the tests check, typed by the generated event types.
import type { Source } from "#/cli/events.gen.ts";

export const HUB = "ssh://git@github.com/me/hub";
const HUB_ID = "5e8a1c3b7d9f2a64";
export const ACME = "https://github.com/acme/skills";
const ACME_ID = "1c9d0b7e4a2f6d38";
const ZETA = "https://gitlab.com/zeta/tools";
const ZETA_ID = "7a3b5c9e1f2d4a60";

const HOUR = 3_600_000;
const DAY = 24 * HOUR;
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

export const hub: Source = {
  access: "writable",
  hub: true,
  id: HUB_ID,
  last_fetched: ago(2 * HOUR),
  skills: 3,
  url: HUB,
};

const acme: Source = {
  access: "read-only",
  commit: "3f2a9c1e8d7b6a",
  id: ACME_ID,
  last_fetched: ago(3 * DAY + 2 * HOUR),
  pin: "v2",
  url: ACME,
};

const zeta: Source = {
  id: ZETA_ID,
  url: ZETA,
};

/** The sources the inventory lists: the hub and both shared sources. */
export const sources: Source[] = [acme, zeta, hub];

/** The sources the inventory lists on a machine with no hub. */
export const sharedOnly: Source[] = [acme, zeta];
