// A machine with about 500 skills, as one with many plugins has: the Skills fixture machine plus
// generated plugin skills and unmanaged skills. The names sort in the order they are generated.
import type { Plugin, SkillNode } from "#/cli/events.gen.ts";

import type { InlineEvent } from "../harness/cli.ts";
import {
  fileReads as baseFileReads,
  filesOf,
  inventory as base,
} from "./skills.ts";

const PLUGINS = 20;
const PLUGIN_SKILLS = 440;
const UNMANAGED_SKILLS = 51;

const padded = (n: number) => String(n).padStart(3, "0");
const pluginName = (n: number) => `pack-${padded(n)}`;
/** The name of the nth generated plugin skill, from 1. */
export const pluginSkillName = (n: number) => `pack-skill-${padded(n)}`;
/** The name of the nth generated unmanaged skill, from 1. */
export const unmanagedSkillName = (n: number) => `stray-skill-${padded(n)}`;

const numbers = (count: number) =>
  Array.from({ length: count }, (_, i) => i + 1);

const plugins: Plugin[] = numbers(PLUGINS).map((n) => ({
  configuration: "claude-code",
  hooks: [],
  logical_id: `plugin:${pluginName(n)}`,
  marketplace: "acme-marketplace",
  name: pluginName(n),
  path: `/home/me/.claude-code/plugins/${pluginName(n)}`,
  physical_id: `plugin:${pluginName(n)}`,
  version: "1.0.0",
}));

const pluginOf = (n: number) => pluginName(((n - 1) % PLUGINS) + 1);

const pluginSkills: SkillNode[] = numbers(PLUGIN_SKILLS).map((n) => {
  const name = pluginSkillName(n);
  const path = `/home/me/.claude-code/plugins/${pluginOf(n)}/skills/${name}`;
  return {
    content_hash: `h-${name}`,
    // Every third skill has no description, so the rows have both heights.
    description: n % 3 === 0 ? "" : `Do the work of ${name}`,
    logical_id: `skill:h-${name}`,
    name,
    occurrences: [
      {
        configuration: "claude-code",
        id: `claude-code:${path}`,
        kind: "directory",
        path,
        plugin: pluginOf(n),
        resolved_path: path,
        scope: "user",
      },
    ],
    physical_id: `skill:h-${name}`,
  };
});

const unmanagedSkills: SkillNode[] = numbers(UNMANAGED_SKILLS).map((n) => {
  const name = unmanagedSkillName(n);
  const path = `/home/me/.cursor/skills/${name}`;
  return {
    content_hash: `h-${name}`,
    description: `Do the work of ${name}`,
    logical_id: `skill:h-${name}`,
    name,
    occurrences: [
      {
        configuration: "cursor",
        id: `cursor:${path}`,
        kind: "directory",
        path,
        resolved_path: path,
        scope: "user",
      },
    ],
    physical_id: `skill:h-${name}`,
  };
});

/** How many skills the inventory holds. */
export const SKILL_COUNT =
  base.skills.length + PLUGIN_SKILLS + UNMANAGED_SKILLS;
/** How many of them are in the PLUGINS group. */
export const PLUGIN_SKILL_COUNT = PLUGIN_SKILLS + 1;
/** How many of them are in the UNMANAGED group. */
export const UNMANAGED_SKILL_COUNT = UNMANAGED_SKILLS + 2;

/** The serve child's first inventory on this machine. */
export const inventory = {
  ...base,
  plugins: [...base.plugins, ...plugins],
  skills: [...base.skills, ...pluginSkills, ...unmanagedSkills],
} satisfies InlineEvent;

/** The `skill files` read of every skill of the inventory, as the app names each one. */
export const fileReads = [
  ...baseFileReads,
  ...numbers(PLUGIN_SKILLS).map((n) => ({
    args: [`${pluginOf(n)}:${pluginSkillName(n)}`],
    events: filesOf(pluginSkillName(n)),
  })),
  ...numbers(UNMANAGED_SKILLS).map((n) => ({
    args: [unmanagedSkillName(n), "--configuration", "cursor"],
    events: filesOf(unmanagedSkillName(n)),
  })),
];
