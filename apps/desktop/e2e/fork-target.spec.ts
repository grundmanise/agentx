// How the Fork dialog names a plugin's skill to `skill fork`, and the name it suggests, as tables
// over the pure functions of fork.ts.
import { expect, test } from "@playwright/test";

import type { LibraryEntry, Occurrence, SkillNode } from "#/cli/events.gen.ts";
import { firstName, forkTarget } from "#/screens/skills/fork.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

const at = (plugin: string, name: string): Occurrence => {
  const path = `/home/me/.claude/plugins/cache/m/${plugin}/1/skills/${name}`;
  return {
    configuration: "claude-code",
    id: path,
    kind: "directory",
    path,
    plugin,
    resolved_path: path,
    scope: "user",
  };
};

const pluginRow = (name: string, occurrences: Occurrence[]): SkillRow => ({
  agents: [],
  description: "",
  enabled: true,
  group: "",
  hasUpdate: false,
  key: name,
  kind: "plugin",
  modified: false,
  name,
  occurrences,
  pills: [],
  published: false,
  universal: false,
});

const node = (occurrences: Occurrence[]): SkillNode => ({
  content_hash: "",
  description: "",
  logical_id: "",
  name: "",
  occurrences,
  physical_id: "",
});

const entry = (name: string): LibraryEntry => ({
  content_hash: "",
  kind: "managed",
  name,
  placements: [],
  universal: [],
});

test("forkTarget: a plugin's skill by its directory alone", () => {
  const o = at("acme", "review");
  expect(forkTarget(pluginRow("review", [o]), [node([o])], [])).toBe("review");
});

test("forkTarget: by plugin when several plugins provide the name", () => {
  const a = at("acme", "review");
  const b = at("tools", "review");
  expect(forkTarget(pluginRow("review", [a]), [node([a]), node([b])], [])).toBe(
    "acme:review"
  );
});

test("forkTarget: by plugin when the library holds the name, which the CLI looks up first", () => {
  const o = at("acme", "review");
  expect(
    forkTarget(pluginRow("review", [o]), [node([o])], [entry("review")])
  ).toBe("acme:review");
});

test("firstName: my-<name>, cut to the limit without a hyphen at its end", () => {
  expect(firstName(pluginRow("pdf", []))).toBe("my-pdf");
  const long = `${"a".repeat(60)}-bcd`;
  const suggested = firstName(pluginRow(long, []));
  expect(suggested).toBe(`my-${"a".repeat(60)}`);
  expect(suggested.length).toBeLessThanOrEqual(64);
});
