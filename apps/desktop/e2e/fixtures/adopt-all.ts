// The Skills fixture's machine with more skills other tools installed: three the Skills CLI added
// (the Skills fixture's scratch among them), two Skillshare added, and one the CLI refuses to
// adopt. Its adoption preview, the `adopt <tool> --all` runs the adopt dialog is tested against,
// and the machine as each run leaves it.
import type {
  AdoptionEvent,
  LibraryEntry,
  ProgressPhase,
  SkillNode,
} from "#/cli/events.gen.ts";

import type { InlineEvent } from "../harness/cli.ts";
import {
  adoption as skillsAdoption,
  inventory as skillsInventory,
} from "./skills.ts";

export const VERCEL = "https://github.com/vercel/skills";
export const TEAM = "https://github.com/team/skills";
const LIBRARY = "/home/me/.agents/skills";
const LOCK = "/home/me/.agents/.skill-lock.json";
const STORE = "/home/me/.config/skillshare/skills/.metadata.json";
const universal = ["codex", "gemini-cli"];

/** The Skills CLI's skills to adopt, the Skills fixture's scratch first. */
export const skillsCliNames = ["scratch", "deck-outline", "commit-style"];
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

/** The machine with `managed` adopted, each from the source of the tool that installed it. */
export const inventoryWith = (managed: string[] = []) =>
  ({
    ...skillsInventory,
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
  schema_version: 4,
  source: tool === "vercel" ? VERCEL : TEAM,
  state,
  subpath: name,
  tool,
  type: "adoption",
  ...(reason === undefined ? {} : { reason }),
});

/**
 * Why the CLI refuses legacy, the same in the preview and in a run of every skill: its record
 * names nowhere to install it from, and the library holds a symlink of it.
 */
const legacyReason = `a symlink to /home/me/work/legacy. To adopt it, move that folder to ${LIBRARY}/legacy`;

/** The record of legacy: refused, with no source, since it names none agentx can install from. */
const legacyRecord: AdoptionEvent = {
  file: LOCK,
  name: "legacy",
  reason: legacyReason,
  schema_version: 4,
  state: "refused",
  tool: "vercel",
  type: "adoption",
};

/** The warning a run of every skill logs for legacy, which it cannot adopt. */
const legacyWarning = `legacy could not be adopted: ${legacyReason}`;

/** The hint of a run that failed because the source no longer holds a skill. */
const movedHint =
  "the source moved or removed it; leave the skill unmanaged, or install another with 'agentx skill add'";

/** What `adopt` would take over with `managed` adopted: every record, in name order. */
export const previewWith = (managed: string[] = []): AdoptionEvent[] =>
  [
    record("api-docs", "skillshare", "candidate"),
    record(MOVED, "vercel", "candidate"),
    record("deck-outline", "vercel", "candidate"),
    record("frontend__ui", "skillshare", "candidate"),
    legacyRecord,
    ...skillsAdoption,
  ].map((r): AdoptionEvent =>
    managed.includes(r.name)
      ? { ...r, reason: "agentx already manages it", state: "managed" }
      : r
  );

/** A preview with no candidate: everything adopted but the record the CLI refuses. */
export const nothingToAdopt = previewWith([
  ...skillsCliNames,
  ...skillshareNames,
]);

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
 * `adopt <tool> --all`'s progress: the fetch of each source it adds, the three steps of an install
 * per skill, then the rescan.
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

/** `adopt skillshare --all`, which adds Skillshare's source, adopts both its skills and exits 0. */
export const skillshareRun: InlineEvent[] = [
  ...progressOf([TEAM], skillshareNames),
  ...skillshareNames.map((name): InlineEvent => ({
    name,
    type: "library_skill",
  })),
  ...skillshareNames.map((name) => record(name, "skillshare", "adopted")),
  { ok: true, summary: "adopted 2 skills", type: "result" },
];

/**
 * `adopt skills-cli --all`, which adds the Skills CLI's source and adopts two of its three
 * skills: the source no longer holds the third, so it exits 5. It covers the record the preview
 * refuses as well.
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
    message: `1 of 3 skills could not be adopted: commit-style: ${movedReason}`,
    type: "error",
  },
];

/** The warnings that run logs: the skill it failed to adopt, then the one it cannot adopt. */
export const skillsCliWarnings = [
  `commit-style: ${movedReason}`,
  legacyWarning,
];

/**
 * `adopt skills-cli --all`, which adopts all three of the Skills CLI's skills and exits 0: the
 * record the preview refuses is a skill it cannot adopt, which it logs as a warning.
 */
export const skillsCliAllRun: InlineEvent[] = [
  ...progressOf([VERCEL], skillsCliNames),
  ...skillsCliNames.map((name): InlineEvent => ({
    name,
    type: "library_skill",
  })),
  record(MOVED, "vercel", "adopted"),
  record("deck-outline", "vercel", "adopted"),
  legacyRecord,
  record("scratch", "vercel", "adopted"),
  {
    ok: true,
    summary: "adopted 3 skills; 1 could not be adopted",
    type: "result",
  },
];

/** The warning that run logs. */
export const skillsCliAllWarnings = [legacyWarning];

/** Why a run that finds none of the Skills CLI's skills in the source fails `name`. */
const goneReason = (name: string) => `${VERCEL} holds no skill at ${name} now`;

/** `adopt skills-cli --all` on a machine where the source holds none of the three: it exits 5. */
export const skillsCliNoneRun: InlineEvent[] = [
  record("commit-style", "vercel", "refused", goneReason("commit-style")),
  record("deck-outline", "vercel", "refused", goneReason("deck-outline")),
  legacyRecord,
  record("scratch", "vercel", "refused", goneReason("scratch")),
  {
    hint: movedHint,
    message: `3 of 3 skills could not be adopted: ${[
      "commit-style",
      "deck-outline",
      "scratch",
    ]
      .map((name) => `${name}: ${goneReason(name)}`)
      .join("; ")}`,
    type: "error",
  },
];
