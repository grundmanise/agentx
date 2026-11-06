// What the Another machine and Skills from the skills CLI sections of Settings are tested against:
// the sources before and after an import, the events of `export`, `import --yes` and
// `adopt --all`, and an adoption preview with two candidates, a refused record and a managed one.
import type { InlineEvent } from "../harness/cli.ts";

export const LIBRARY = "/home/me/.agents/skills";
export const ACCOUNT = "https://github.com/me/agentx-account";
export const NEW_ACCOUNT = "https://github.com/me/work-account";
export const TEAM = "https://github.com/team/skills";
export const VENDOR = "https://github.com/vendor/skills";

/** The file the user picks in the open dialog. */
export const SNAPSHOT = "/home/me/Downloads/agentx-export.json";

/** This machine's sources before the import: its account remote and one shared source, both fetched. */
export const sourcesBefore: InlineEvent[] = [
  {
    access: "writable",
    account: true,
    id: "s-account",
    last_fetched: "2026-11-05T10:00:00Z",
    skills: 3,
    type: "source",
    url: ACCOUNT,
  },
  {
    commit: "a1a1a1a1a1a1",
    id: "s-vendor",
    last_fetched: "2026-11-05T10:00:00Z",
    type: "source",
    url: VENDOR,
  },
];

/**
 * The sources after the import: the imported settings set another account remote and a pinned
 * team source, neither fetched here, and keep the vendor source, which this machine has fetched.
 */
export const sourcesAfterImport: InlineEvent[] = [
  {
    account: true,
    id: "s-new-account",
    last_fetched: "2026-11-01T09:00:00Z",
    type: "source",
    url: NEW_ACCOUNT,
  },
  {
    id: "s-team",
    last_fetched: "2026-11-01T09:00:00Z",
    pin: "v2",
    type: "source",
    url: TEAM,
  },
  {
    commit: "a1a1a1a1a1a1",
    id: "s-vendor",
    last_fetched: "2026-11-05T10:00:00Z",
    type: "source",
    url: VENDOR,
  },
];

/** Every source fetched: what `source list` reports once Add sources has added them. */
export const sourcesAdded: InlineEvent[] = sourcesAfterImport.map((s) => {
  if (s.type !== "source") {
    return s;
  }
  return s.account === true
    ? { ...s, skills: 2 }
    : { ...s, commit: s.commit ?? "b2b2b2b2b2b2" };
});

export const exported: InlineEvent[] = [
  { path: `${LIBRARY}/agentx-export.json`, skills: 3, type: "export" },
];

export const alreadyThere: InlineEvent[] = [
  {
    code: "refused",
    hint: "give another name, or pass --force to overwrite it",
    message: `${LIBRARY}/agentx-export.json is already there`,
    type: "error",
  },
];

/** What `import <file> --yes` reports: the settings it wrote, then one `import_skill` per entry. */
export const imported: InlineEvent[] = [
  { type: "settings" },
  {
    action: "installed",
    kind: "managed",
    name: "pdf",
    placements: ["claude-code"],
    source: VENDOR,
    state: "missing",
    subpath: "skills/pdf",
    type: "import_skill",
  },
  {
    action: "kept",
    kind: "managed",
    name: "review",
    placements: [],
    source: NEW_ACCOUNT,
    state: "present",
    type: "import_skill",
  },
  {
    action: "skipped",
    kind: "unmanaged",
    name: "notes",
    placements: [],
    reason: "notes is unmanaged, so there is no source to install it from",
    state: "missing",
    type: "import_skill",
  },
];

/** The adoption preview: two candidates, one refused record and one agentx manages already. */
export const adoptionPreview: InlineEvent[] = [
  {
    file: "/home/me/.agents/.skill-lock.json",
    name: "brand",
    reason: "adopting adds and fetches https://github.com/acme/skills",
    source: "https://github.com/acme/skills",
    state: "candidate",
    subpath: "brand",
    tool: "vercel",
    type: "adoption",
  },
  {
    file: "/home/me/.agents/.skill-lock.json",
    name: "frontend",
    source: VENDOR,
    state: "candidate",
    subpath: "frontend",
    tool: "vercel",
    type: "adoption",
  },
  {
    file: "/home/me/.agents/.skill-lock.json",
    name: "local-notes",
    reason:
      "it was installed from a local folder, which is no source agentx can read",
    state: "refused",
    tool: "vercel",
    type: "adoption",
  },
  {
    file: "/home/me/.agents/.skill-lock.json",
    name: "pdf",
    source: VENDOR,
    state: "managed",
    subpath: "skills/pdf",
    tool: "vercel",
    type: "adoption",
  },
];

/** `adopt --all`: one progress step per source and per install step, then the records. */
export const adoptedAll: InlineEvent[] = [
  { current: 1, phase: "fetch", total: 8, type: "progress" },
  { current: 2, phase: "blobs", total: 8, type: "progress" },
  ...adoptionPreview.map((record) =>
    record.type === "adoption" && record.state === "candidate"
      ? { ...record, reason: undefined, state: "adopted" as const }
      : record
  ),
];
