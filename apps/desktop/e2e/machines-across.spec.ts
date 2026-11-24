// One skill on two machines, as a table over the pure functions: whether two machines hold the
// same skill (`sameSkill`) and, when they do, whether the same version of it (`versionOf`). The
// By skill view of the Machines screen and the Also on line of a skill's details both read this.
import { expect, test } from "@playwright/test";

import type { ReportLibrarySkill } from "#/cli/events.gen.ts";
import { sameSkill, versionOf } from "#/screens/machines/across.ts";
import { holdingOf } from "#/screens/machines/projection.ts";
import type { Holding } from "#/screens/machines/projection.ts";

/** What the lineage of a skill of a source names: the source, and the directory in it. */
const SOURCE = {
  source: "https://github.com/example/skills",
  subpath: "skills/changelog",
};

/** The logical id the CLI gives an entry: its fork id, else its source and subpath, else its content. */
const logicalId = (facts: Partial<ReportLibrarySkill>) => {
  if (facts.fork_id !== undefined) {
    return `skill:fork:${facts.fork_id}`;
  }
  return facts.source !== undefined && facts.subpath !== undefined
    ? `skill:source:${facts.source}:${facts.subpath}`
    : `skill:content:${facts.content_hash ?? ""}`;
};

/** A library entry as a machine reports it. */
const entry = (
  name: string,
  facts: Partial<ReportLibrarySkill> = {}
): Holding => holdingOf({ logical_id: logicalId(facts), name, ...facts });

/** A skill no library manages: a plugin's, or one of an agent's own skills directory. */
const unmanaged = (name: string, contentHash: string): Holding => ({
  contentHash,
  logicalId: undefined,
  name,
  upstreamCommit: undefined,
});

/** What the two hold: another skill, or the same or another version of one skill. */
const relation = (a: Holding, b: Holding) =>
  sameSkill(a, b) ? `${versionOf(a, b)} version` : "another skill";

const cases: { name: string; a: Holding; b: Holding; expected: string }[] = [
  {
    a: entry("changelog", { content_hash: "h1", ...SOURCE }),
    b: entry("changelog", { content_hash: "h1", ...SOURCE }),
    expected: "same version",
    name: "one logical id and one content",
  },
  {
    a: entry("changelog", { content_hash: "h1", ...SOURCE }),
    b: entry("changelog", { content_hash: "h2", ...SOURCE }),
    expected: "other version",
    name: "one logical id, edited on one machine",
  },
  {
    a: entry("release-notes", { content_hash: "h1", fork_id: "f1" }),
    b: entry("notes", { content_hash: "h1", fork_id: "f1" }),
    expected: "same version",
    name: "one fork id under two names",
  },
  {
    a: entry("notes", { content_hash: "h1", fork_id: "f1" }),
    b: entry("notes", { content_hash: "h1", fork_id: "f2" }),
    expected: "another skill",
    name: "one name with two logical ids",
  },
  {
    a: entry("scratch", { content_hash: "h1", ...SOURCE }),
    b: unmanaged("scratch", "h1"),
    expected: "same version",
    name: "a library skill and a skill no library manages, by name",
  },
  {
    a: unmanaged("scratch", "h1"),
    b: unmanaged("scratch", "h2"),
    expected: "other version",
    name: "two skills no library manages, edited on one machine",
  },
  {
    // With neither a fork id nor a source, the logical id is the content: it tells no skill.
    a: entry("draft", { content_hash: "h1" }),
    b: entry("draft", { content_hash: "h2" }),
    expected: "other version",
    name: "library skills that name no fork id and no source, by name",
  },
  {
    a: entry("draft", { content_hash: "h1" }),
    b: entry("outline", { content_hash: "h1" }),
    expected: "another skill",
    name: "one content under two names, with no fork id and no source",
  },
  {
    // A source with no directory in it does not make the id either.
    a: entry("draft", { content_hash: "h1", source: SOURCE.source }),
    b: entry("draft", { content_hash: "h2", source: SOURCE.source }),
    expected: "other version",
    name: "library skills that name a source and no subpath, by name",
  },
  {
    a: entry("changelog", {
      content_hash: "h1",
      ...SOURCE,
      upstream_commit: "c1",
    }),
    b: entry("changelog", { ...SOURCE, upstream_commit: "c1" }),
    expected: "same version",
    name: "no content hash on the cloud machine, one commit",
  },
  {
    a: entry("changelog", {
      content_hash: "h1",
      ...SOURCE,
      upstream_commit: "c2",
    }),
    b: entry("changelog", { ...SOURCE, upstream_commit: "c1" }),
    expected: "other version",
    name: "no content hash on the cloud machine, two commits",
  },
  {
    a: entry("changelog", { content_hash: "h1", ...SOURCE }),
    b: entry("changelog", { ...SOURCE, upstream_commit: "c1" }),
    expected: "same version",
    name: "nothing to compare claims no difference",
  },
  {
    a: entry("changelog", {
      content_hash: "h1",
      ...SOURCE,
      upstream_commit: "c1",
    }),
    b: entry("changelog", {
      content_hash: "h1",
      ...SOURCE,
      upstream_commit: "c2",
    }),
    expected: "same version",
    name: "one content installed at two commits",
  },
];

for (const c of cases) {
  test(`${c.name}: ${c.expected}`, () => {
    expect(relation(c.a, c.b)).toBe(c.expected);
    expect(relation(c.b, c.a)).toBe(c.expected);
  });
}
