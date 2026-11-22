// adoptArgs, as a table: the `skill import` command line that takes over each unmanaged skill, or
// none for a skill `skill import` has no way to take.
import { expect, test } from "@playwright/test";

import type {
  AdoptionEvent,
  InventoryEvent,
  SkillNode,
} from "#/cli/events.gen.ts";
import { adoptArgs } from "#/screens/skills/placements.ts";
import { projectRows } from "#/screens/skills/projection.ts";

import { inventory as refusedInventory } from "./fixtures/adoption.ts";
import { inventory, linked, meetingNotes } from "./fixtures/skills.ts";

/** The record the Skills CLI keeps of `name`, in the `state` the adoption preview gives it. */
const record = (
  name: string,
  state: AdoptionEvent["state"]
): AdoptionEvent => ({
  file: "/home/me/.agents/.skill-lock.json",
  name,
  schema_version: 4,
  state,
  tool: "vercel",
  type: "adoption",
});

/** A skill node with one occurrence in Cursor: `fields` say its path, kind and scope. */
const inCursor = (
  name: string,
  fields: Partial<SkillNode["occurrences"][number]>
): SkillNode => ({
  content_hash: `h-own-${name}`,
  description: "",
  logical_id: `skill:h-own-${name}`,
  name,
  occurrences: [
    {
      configuration: "cursor",
      id: `cursor:${name}`,
      kind: "directory",
      path: `/home/me/.cursor/skills/${name}`,
      resolved_path: `/home/me/.cursor/skills/${name}`,
      scope: "user",
      ...fields,
    },
  ],
  physical_id: `skill:h-own-${name}`,
});

/** Cursor's own directory called pdf, with other content than the library's skill of that name. */
const ownPdf = inCursor("pdf", {});
/** A skill of a project Cursor is opened in. */
const inProject = inCursor("project-notes", {
  path: "/home/me/work/app/.cursor/skills/project-notes",
  resolved_path: "/home/me/work/app/.cursor/skills/project-notes",
  scope: "project",
});

/** The row `key` names on the fixture machine with `more` skills, by the adoption preview `records`. */
const rowOf = (
  key: string,
  records: AdoptionEvent[],
  machine: Pick<
    InventoryEvent,
    "configurations" | "library" | "plugins" | "skills"
  > = inventory,
  more: SkillNode[] = []
) => {
  const whole: InventoryEvent = {
    edges: [],
    instance_id: "instance-1",
    machine: { id: "machine-1", label: "studio-mac" },
    mcp_servers: [],
    scan_counter: 1,
    schema_version: 4,
    type: "inventory",
    warnings: [],
    ...machine,
    skills: [...machine.skills, ...more],
  };
  const rows = projectRows({
    adoption: records,
    inventory: whole,
    updates: {},
  });
  const row = rows.find((r) => r.key === key);
  if (row === undefined) {
    throw new Error(`no row ${key}`);
  }
  return row;
};

// [what the skill is, its row, the command line]
const table: [string, ReturnType<typeof rowOf>, string[] | undefined][] = [
  [
    "a library skill another tool recorded",
    rowOf("scratch", [record("scratch", "candidate")]),
    ["skill", "import", "scratch"],
  ],
  [
    "a library skill no tool recorded",
    rowOf("scratch", []),
    ["skill", "import", "scratch"],
  ],
  [
    "a library skill whose record the CLI refuses",
    rowOf("legacy", [record("legacy", "refused")], refusedInventory),
    undefined,
  ],
  [
    "a library skill whose record says agentx manages it",
    rowOf("scratch", [record("scratch", "managed")]),
    undefined,
  ],
  [
    "a directory in a client's own skills directory",
    rowOf("skill:h-notes", []),
    ["skill", "import", "notes"],
  ],
  [
    "a directory whose name is not the skill's frontmatter name",
    rowOf("skill:h-meeting", [], inventory, [meetingNotes]),
    ["skill", "import", "meeting-notes"],
  ],
  [
    "a directory under a name the library holds another skill by",
    rowOf("skill:h-own-pdf", [], inventory, [ownPdf]),
    undefined,
  ],
  [
    "a symlink in a client's own skills directory",
    rowOf("skill:h-linked", [], inventory, [linked]),
    undefined,
  ],
  [
    "a skill of a project",
    rowOf("skill:h-own-project-notes", [], inventory, [inProject]),
    undefined,
  ],
  ["a plugin's skill", rowOf("skill:h-commit", []), undefined],
  ["a skill of a source", rowOf("pdf", []), undefined],
];

for (const [what, row, want] of table) {
  test(`adoptArgs(${what})`, () => {
    expect(adoptArgs(row)).toEqual(want);
  });
}
