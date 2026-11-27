// importOf, as a table: the `skill import` command line that takes over each unmanaged skill, or
// why `skill import` has no way to take it.
import { expect, test } from "@playwright/test";

import type {
  Adoptable,
  AdoptionRefusal,
  InventoryEvent,
  SkillNode,
} from "#/cli/events.gen.ts";
import { adoptArgs, importOf } from "#/screens/skills/placements.ts";
import { projectRows } from "#/screens/skills/projection.ts";

import { inventory as refusedInventory } from "./fixtures/adoption.ts";
import { inventory, linked, meetingNotes } from "./fixtures/skills.ts";
import { thisMachine } from "./harness/cli.ts";

/** What the inventory says of `name`, which the Skills CLI installed: to adopt, or refused for `reason`. */
const recorded = (name: string, reason?: AdoptionRefusal): Adoptable[] => [
  reason === undefined
    ? { skills: [name], tool: "vercel" }
    : { refused: [{ name, reason }], skills: [], tool: "vercel" },
];

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
/** Cursor's own directory whose name reads as a flag. */
const dashed = inCursor("--all", {});

/** The row `key` names on the fixture machine with `more` skills, where the inventory lists `adoptable`. */
const rowOf = (
  key: string,
  adoptable: Adoptable[],
  machine: Pick<
    InventoryEvent,
    "configurations" | "library" | "plugins" | "skills"
  > = inventory,
  more: SkillNode[] = []
) => {
  const whole: InventoryEvent = {
    edges: [],
    installs: [],
    instance_id: "instance-1",
    machine: thisMachine,
    mcp_servers: [],
    scan_counter: 1,
    schema_version: 5,
    settings: { ignore_system_files: true },
    sources: [],
    type: "inventory",
    warnings: [],
    ...machine,
    adoptable,
    skills: [...machine.skills, ...more],
  };
  const rows = projectRows({ inventory: whole });
  const row = rows.find((r) => r.key === key);
  if (row === undefined) {
    throw new Error(`no row ${key}`);
  }
  return row;
};

// [what the skill is, its row, the command line, or a distinctive part of why there is none]
const table: [
  string,
  ReturnType<typeof rowOf>,
  string[] | string | undefined,
][] = [
  [
    "a library skill another tool recorded",
    rowOf("scratch", recorded("scratch")),
    ["skill", "import", "scratch"],
  ],
  [
    "a library skill no tool recorded",
    rowOf("scratch", []),
    ["skill", "import", "scratch"],
  ],
  [
    "a library skill whose record the CLI refuses",
    rowOf("legacy", recorded("legacy", "symlink"), refusedInventory),
    "It's a link",
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
    "a directory whose name starts with a dash, after --",
    rowOf("skill:h-own---all", [], inventory, [dashed]),
    ["skill", "import", "--", "--all"],
  ],
  [
    "a directory under a name the library holds another skill by",
    rowOf("skill:h-own-pdf", [], inventory, [ownPdf]),
    "Your library already has a skill named pdf",
  ],
  [
    "a symlink in a client's own skills directory",
    rowOf("skill:h-linked", [], inventory, [linked]),
    "It's a link",
  ],
  [
    "a skill of a project",
    rowOf("skill:h-own-project-notes", [], inventory, [inProject]),
    "It's in a project's folder",
  ],
  ["a plugin's skill", rowOf("skill:h-commit", []), undefined],
  ["a skill of a source", rowOf("pdf", []), undefined],
];

for (const [what, row, want] of table) {
  test(`importOf(${what})`, () => {
    const result = importOf(row);
    if (typeof want === "string") {
      expect(
        result !== undefined && "why" in result ? result.why : ""
      ).toContain(want);
    } else {
      expect(adoptArgs(row)).toEqual(want);
    }
  });
}

test("importOf names the library skill in the way of a folder", () => {
  expect(importOf(rowOf("skill:h-own-pdf", [], inventory, [ownPdf]))).toEqual({
    inTheWay: "pdf",
    why: "Your library already has a skill named pdf, this folder's name. Import or remove that skill first",
  });
});
