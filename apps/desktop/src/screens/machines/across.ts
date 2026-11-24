// One skill across machines: which machines hold it, and where its version is another one. A
// report states neither, so all of it is worked out here, from what each machine holds
// (projection.ts), for the By skill view of the Machines screen and the Also on line of a
// skill's details. Pure functions over one rule, `sameSkill` and `versionOf`.
import { sorted } from "#/screens/machines/projection.ts";
import type {
  HeldSkill,
  Holding,
  MachineView,
  SkillOrigin,
} from "#/screens/machines/projection.ts";

/**
 * Whether two machines hold the same skill: their library entries carry the same logical id. When
 * either has no logical id, a skill no library manages or one whose entry names neither a fork id
 * nor a source, they are the same skill when their names are equal.
 */
export const sameSkill = (a: Holding, b: Holding) =>
  a.logicalId !== undefined && b.logicalId !== undefined
    ? a.logicalId === b.logicalId
    : a.name === b.name;

/** The same version of a skill, or another one. */
export type Version = "same" | "other";

/**
 * How two holdings of the same skill compare: by their content when both have a content hash,
 * else by the commit of the source they were installed at when both name one. With nothing to
 * compare, as for a skill of the cloud machine, which nothing has read, they count as the same:
 * the skill is there, and no difference is claimed that cannot be shown.
 */
export const versionOf = (a: Holding, b: Holding): Version => {
  if (a.contentHash !== undefined && b.contentHash !== undefined) {
    return a.contentHash === b.contentHash ? "same" : "other";
  }
  if (a.upstreamCommit !== undefined && b.upstreamCommit !== undefined) {
    return a.upstreamCommit === b.upstreamCommit ? "same" : "other";
  }
  return "same";
};

/**
 * How what one machine holds of a skill compares with what another holds of it. A machine can
 * hold a name twice, from a plugin and in an agent's own skills directory: the two machines have
 * the same version when any of the one's is any of the other's.
 */
const compare = (
  reference: readonly Holding[],
  theirs: readonly Holding[]
): Version =>
  theirs.some((t) => reference.some((r) => versionOf(r, t) === "same"))
    ? "same"
    : "other";

/** Another machine that holds a skill, and how its version compares. */
export interface Elsewhere {
  machine: MachineView;
  version: Version;
}

/**
 * The other machines that hold `skill`, a skill of this machine, in the order the Machines screen
 * lists them: the other machines by label, then the cloud machine.
 */
export const alsoOn = (
  skill: Holding,
  machines: readonly MachineView[]
): Elsewhere[] =>
  machines.flatMap((machine) => {
    const theirs = machine.held.filter((h) => sameSkill(skill, h));
    return machine.kind === "this" || theirs.length === 0
      ? []
      : [{ machine, version: compare([skill], theirs) }];
  });

/** What a machine has of a skill: the reference's version, another version, or nothing. */
export type Cell = Version | "none";

/** One skill, with what each machine has of it. */
export interface AcrossRow {
  key: string;
  name: string;
  /** What brings it to the machine the others are compared with. */
  origin: SkillOrigin;
  /** One cell per machine, in the machines' order. */
  cells: Cell[];
  /** Some machine does not hold it, or holds another version. */
  differs: boolean;
}

const byName = (a: AcrossRow, b: AcrossRow) =>
  a.name.localeCompare(b.name, undefined, { sensitivity: "base" }) ||
  a.key.localeCompare(b.key);

/**
 * Every skill any of `machines` holds, one row each, A to Z by name. A row is a logical id, or a
 * name that no logical id goes by. Each machine is compared with the first one that holds the
 * skill: this machine, which comes first, whenever it does.
 */
export const skillsAcross = (machines: readonly MachineView[]): AcrossRow[] => {
  const all = machines.flatMap((m) => m.held);
  // One holding stands for each skill: the first of each logical id, then the first of each name
  // that none of those has.
  const anchors = new Map<string, HeldSkill>();
  for (const h of all) {
    if (h.logicalId !== undefined && !anchors.has(h.logicalId)) {
      anchors.set(h.logicalId, h);
    }
  }
  const managed = new Set([...anchors.values()].map((h) => h.name));
  for (const h of all) {
    const key = `name ${h.name}`;
    if (
      h.logicalId === undefined &&
      !managed.has(h.name) &&
      !anchors.has(key)
    ) {
      anchors.set(key, h);
    }
  }
  const rows = [...anchors].flatMap(([key, anchor]): AcrossRow[] => {
    const held = machines.map((m) =>
      m.held.filter((h) => sameSkill(anchor, h))
    );
    const reference = held.find((h) => h.length > 0);
    const first = reference?.[0];
    if (reference === undefined || first === undefined) {
      return [];
    }
    const cells = held.map((h): Cell => {
      if (h.length === 0) {
        return "none";
      }
      return h === reference ? "same" : compare(reference, h);
    });
    return [
      {
        cells,
        differs: cells.some((c) => c !== "same"),
        key,
        name: first.name,
        origin: {
          from: first.from,
          plugin: first.plugin,
          source: first.source,
        },
      },
    ];
  });
  return sorted(rows, byName);
};
