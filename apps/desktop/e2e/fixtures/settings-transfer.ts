// What the Export settings and Import settings sections of Settings are tested against: the
// sources before and after an import, and the events of `export`, `import --yes` and a
// `source add` that cannot reach its source.
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

/** The reason the CLI gives for a skipped unmanaged skill. */
export const UNMANAGED_REASON =
  "notes is unmanaged, so there is no source to install it from";

/**
 * What `import <file> --yes` reports: the settings it wrote, then one `import_skill` per entry,
 * two installed, one updated, one kept, one placed and two skipped.
 */
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
    action: "installed",
    kind: "managed",
    name: "review-kit",
    placements: ["claude-code"],
    source: VENDOR,
    state: "missing",
    subpath: "skills/review-kit",
    type: "import_skill",
  },
  {
    action: "updated",
    kind: "managed",
    name: "docx",
    placements: ["claude-code"],
    source: VENDOR,
    state: "different",
    subpath: "skills/docx",
    type: "import_skill",
  },
  {
    action: "kept",
    kind: "managed",
    name: "brand",
    placements: [],
    source: NEW_ACCOUNT,
    state: "present",
    type: "import_skill",
  },
  {
    action: "placed",
    kind: "managed",
    name: "slides",
    placements: ["cursor"],
    source: VENDOR,
    state: "present",
    subpath: "skills/slides",
    type: "import_skill",
  },
  {
    action: "skipped",
    kind: "unmanaged",
    name: "notes",
    placements: [],
    reason: UNMANAGED_REASON,
    state: "missing",
    type: "import_skill",
  },
  {
    action: "skipped",
    kind: "managed",
    name: "drafts",
    placements: [],
    reason: `${TEAM} is not fetched on this machine`,
    source: TEAM,
    state: "missing",
    type: "import_skill",
  },
];

/** An import that installed, updated and skipped nothing: one skill kept, one placed. */
export const nothingNew: InlineEvent[] = imported.filter(
  (e) =>
    e.type === "settings" ||
    (e.type === "import_skill" &&
      (e.action === "kept" || e.action === "placed"))
);

/** `source add` refusing a source git cannot reach. */
export const unreachable: InlineEvent[] = [
  {
    code: "source",
    hint: "check the URL and your network, then run the command again",
    message: `could not reach ${TEAM}`,
    type: "error",
  },
];
