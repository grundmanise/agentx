// The Skills fixture's machine with more skills other tools installed: three the Skills CLI added
// (the Skills fixture's scratch among them), two Skillshare added, and one the CLI refuses to
// adopt. The `skill import --all` runs the adopt dialog is tested against, of one tool and of
// both, and the machine as each run leaves it.
import type {
  Adoptable,
  AdoptionEvent,
  LibraryEntry,
  ProgressPhase,
  SkillNode,
} from "#/cli/events.gen.ts";

import type { InlineEvent } from "../harness/cli.ts";
import { inventory as skillsInventory } from "./skills.ts";

export const VERCEL = "https://github.com/vercel/skills";
export const TEAM = "https://github.com/team/skills";
const LIBRARY = "/home/me/.agents/skills";
const LOCK = "/home/me/.agents/.skill-lock.json";
const STORE = "/home/me/.config/skillshare/skills/.metadata.json";
const universal = ["codex", "gemini-cli"];

/** The Skills CLI's skills to adopt, in name order. */
export const skillsCliNames = ["commit-style", "deck-outline", "scratch"];
/** Skillshare's skills to adopt. */
export const skillshareNames = ["api-docs", "frontend__ui"];
/** The Skills CLI's skill whose source no longer holds it, so a run fails to adopt it. */
export const MOVED = "commit-style";
export const movedReason = `${VERCEL} holds no skill at commit-style now`;

const libraryPlacements = (name: string) =>
  universal.map((id) => ({
    configuration: id,
    kind: "library" as const,
    mode: "library" as const,
    path: `${LIBRARY}/${name}`,
  }));

/** A library directory another tool installed, which agentx does not manage yet. */
const unmanaged = (name: string): LibraryEntry => ({
  content_hash: `h-${name}`,
  kind: "unmanaged",
  name,
  placements: libraryPlacements(name),
  universal,
});

/** The same directory once adopted: a skill of the source the tool installed it from. */
const adopted = (name: string, source: string): LibraryEntry => ({
  base_hash: `h-${name}`,
  content_hash: `h-${name}`,
  kind: "managed",
  name,
  placements: libraryPlacements(name),
  source,
  state: "current",
  subpath: name,
  universal,
  upstream_commit: "7c7c7c7c7c7c",
});

const node = (name: string): SkillNode => ({
  content_hash: `h-${name}`,
  description: `Use this skill to ${name.replace("__", " ")}`,
  logical_id: `skill:h-${name}`,
  name,
  occurrences: universal.map((id) => ({
    configuration: id,
    id: `${id}:${LIBRARY}/${name}`,
    kind: "directory" as const,
    path: `${LIBRARY}/${name}`,
    resolved_path: `${LIBRARY}/${name}`,
    scope: "user" as const,
  })),
  physical_id: `skill:h-${name}`,
});

/** The names this fixture adds to the Skills fixture's library; scratch is there already. */
const added = ["deck-outline", MOVED, ...skillshareNames, "legacy"];

/** The records of `tool` that name `source`, with `managed` adopted. */
const recordsOf = (
  tool: Adoptable["tool"],
  source: string,
  names: string[],
  managed: string[]
): Adoptable => {
  const taken = names.filter((name) => managed.includes(name));
  return {
    skills: names.filter((name) => !managed.includes(name)),
    source,
    tool,
    ...(taken.length === 0 ? {} : { managed: taken }),
  };
};

/**
 * What the tools' records hold with `managed` adopted: legacy, whose record names no source and
 * which the library holds as a symlink, then the skills of each tool, by the source they name.
 */
const adoptableWith = (managed: string[]): Adoptable[] => [
  {
    refused: [
      { name: "legacy", path: "/home/me/work/legacy", reason: "symlink" },
    ],
    skills: [],
    tool: "vercel",
  },
  recordsOf("vercel", VERCEL, skillsCliNames, managed),
  recordsOf("skillshare", TEAM, skillshareNames, managed),
];

/** The machine with `managed` adopted, each from the source of the tool that installed it. */
export const inventoryWith = (managed: string[] = []) =>
  ({
    ...skillsInventory,
    adoptable: adoptableWith(managed),
    library: [
      ...skillsInventory.library.filter((e) => !managed.includes(e.name)),
      ...added
        .filter((name) => !managed.includes(name))
        .map((name) => unmanaged(name)),
      ...managed.map((name) =>
        adopted(name, skillshareNames.includes(name) ? TEAM : VERCEL)
      ),
    ],
    scan_counter: managed.length + 1,
    skills: [...skillsInventory.skills, ...added.map((name) => node(name))],
  }) satisfies InlineEvent;

const record = (
  name: string,
  tool: AdoptionEvent["tool"],
  state: AdoptionEvent["state"],
  reason?: string
): AdoptionEvent => ({
  file: tool === "vercel" ? LOCK : STORE,
  name,
  schema_version: 6,
  source: tool === "vercel" ? VERCEL : TEAM,
  state,
  subpath: name,
  tool,
  type: "adoption",
  ...(reason === undefined ? {} : { reason }),
});

/**
 * Why a run of every skill refuses legacy: its record names nowhere to install it from, and the
 * library holds a symlink of it.
 */
const legacyReason = `a symlink to /home/me/work/legacy. To import it, move that folder to ${LIBRARY}/legacy`;

/** The record of legacy: refused, with no source, since it names none agentx can install from. */
const legacyRecord: AdoptionEvent = {
  file: LOCK,
  name: "legacy",
  reason: legacyReason,
  schema_version: 6,
  state: "refused",
  tool: "vercel",
  type: "adoption",
};

/** Why a run that does not find the Skills CLI's skill `name` in the source fails it. */
const goneReason = (name: string) => `${VERCEL} holds no skill at ${name} now`;

/** The warning a run of every skill logs for legacy, which it cannot adopt. */
const legacyWarning = `legacy could not be imported: ${legacyReason}`;

/** The hint of a run that failed because the source no longer holds a skill. */
const movedHint =
  "the source moved or removed it; leave the skill unmanaged, or install another with 'agentx skill add'";

const progress = (
  phase: ProgressPhase,
  subject: string | undefined,
  current: number,
  total: number
): InlineEvent => ({
  current,
  phase,
  total,
  type: "progress",
  ...(subject === undefined ? {} : { subject }),
});

/**
 * `skill import --from <tool> --all`'s progress: the fetch of each source it adds, the three
 * steps of an install per skill, then the rescan.
 */
const progressOf = (sources: string[], names: string[]): InlineEvent[] => {
  const total = sources.length + 3 * names.length + 1;
  return [
    ...sources.map((url, i) => progress("fetch", url, i + 1, total)),
    ...names.flatMap((name, i) =>
      (["blobs", "import", "install"] as const).map((phase, step) =>
        progress(phase, name, sources.length + 3 * i + step + 1, total)
      )
    ),
    progress("rescan", undefined, total, total),
  ];
};

/**
 * `skill import --from skillshare --all`, which adds Skillshare's source, adopts both its skills
 * and exits 0.
 */
export const skillshareRun: InlineEvent[] = [
  ...progressOf([TEAM], skillshareNames),
  ...skillshareNames.map((name): InlineEvent => ({
    name,
    type: "library_skill",
  })),
  ...skillshareNames.map((name) => record(name, "skillshare", "adopted")),
  { ok: true, summary: "imported 2 skills", type: "result" },
];

/**
 * `skill import --from skills-cli --all`, which adds the Skills CLI's source and adopts two of
 * its three skills: the source no longer holds commit-style, so it exits 5. It covers the record it
 * cannot adopt as well.
 */
export const skillsCliRun: InlineEvent[] = [
  ...progressOf([VERCEL], ["deck-outline", "scratch"]),
  { name: "deck-outline", type: "library_skill" },
  { name: "scratch", type: "library_skill" },
  record("commit-style", "vercel", "refused", movedReason),
  record("deck-outline", "vercel", "adopted"),
  legacyRecord,
  record("scratch", "vercel", "adopted"),
  {
    hint: movedHint,
    message: `1 of 3 skills could not be imported: commit-style: ${movedReason}`,
    type: "error",
  },
];

/** The warnings that run logs: the skill it failed to adopt, then the one it cannot adopt. */
export const skillsCliWarnings = [
  `commit-style: ${movedReason}`,
  legacyWarning,
];

/**
 * `skill import --from skills-cli --from skillshare --all`, one run over both tools: it adds both
 * sources and adopts every skill but `gone`, the ones the Skills CLI's source no longer holds. The
 * sources and the skills come in name order, whichever tool installed them: each source's fetch,
 * then the `blobs` step of its skills, then every `import` step, every `install` step and the one
 * rescan. The record of legacy is a skill it cannot adopt, which it logs as a warning
 * and fails nothing; a skill that is gone fails the run, which exits 5.
 */
export const bothToolsRun = (gone: string[] = []): InlineEvent[] => {
  const vercel = skillsCliNames.filter((name) => !gone.includes(name));
  const names = [...skillshareNames, ...vercel];
  const total = 2 + 3 * names.length + 1;
  const steps: [ProgressPhase, string | undefined][] = [
    ["fetch", TEAM],
    ...skillshareNames.map((name): [ProgressPhase, string] => ["blobs", name]),
    ["fetch", VERCEL],
    ...vercel.map((name): [ProgressPhase, string] => ["blobs", name]),
    ...names.map((name): [ProgressPhase, string] => ["import", name]),
    ...names.map((name): [ProgressPhase, string] => ["install", name]),
    ["rescan", undefined],
  ];
  const adoption = (name: string, tool: AdoptionEvent["tool"]) =>
    gone.includes(name)
      ? record(name, tool, "refused", goneReason(name))
      : record(name, tool, "adopted");
  const ended: InlineEvent =
    gone.length === 0
      ? {
          ok: true,
          summary: `imported ${names.length} skills; 1 could not be imported`,
          type: "result",
        }
      : {
          hint: movedHint,
          message: `${gone.length} of ${names.length + gone.length} skills could not be imported: ${gone
            .map((name) => `${name}: ${goneReason(name)}`)
            .join("; ")}`,
          type: "error",
        };
  return [
    ...steps.map(([phase, subject], i) =>
      progress(phase, subject, i + 1, total)
    ),
    ...names.map((name): InlineEvent => ({ name, type: "library_skill" })),
    adoption("api-docs", "skillshare"),
    adoption("commit-style", "vercel"),
    adoption("deck-outline", "vercel"),
    adoption("frontend__ui", "skillshare"),
    legacyRecord,
    adoption("scratch", "vercel"),
    ended,
  ];
};

/** The warnings that run logs: each skill it failed to adopt, then the one it cannot adopt. */
export const bothToolsWarnings = (gone: string[] = []) => [
  ...gone.map((name) => `${name}: ${goneReason(name)}`),
  legacyWarning,
];

/**
 * `skill import --from skills-cli --all` on a machine where the source holds none of the three: it
 * exits 5.
 */
export const skillsCliNoneRun: InlineEvent[] = [
  record("commit-style", "vercel", "refused", goneReason("commit-style")),
  record("deck-outline", "vercel", "refused", goneReason("deck-outline")),
  legacyRecord,
  record("scratch", "vercel", "refused", goneReason("scratch")),
  {
    hint: movedHint,
    message: `3 of 3 skills could not be imported: ${[
      "commit-style",
      "deck-outline",
      "scratch",
    ]
      .map((name) => `${name}: ${goneReason(name)}`)
      .join("; ")}`,
    type: "error",
  },
];
