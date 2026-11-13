// The machine the Sources tests run against: two shared sources (one pinned and read-only here,
// fetched three days ago, one never fetched) and an account remote; the listing of the first source; a library that puts one of
// its skills in each state a card can be in; three configurations that take placements (two
// enabled) and two universal ones; your skills on the account remote; and a search answer.
// Inline events with only the fields the tests check, typed by the generated event types.
import type {
  Configuration,
  InstallableSkillEvent,
  LibraryEntry,
  SearchResult,
  SourceEvent,
  SourceSkillEvent,
} from "#/cli/events.gen.ts";

import { configuration } from "../harness/cli.ts";
import type { InlineEvent } from "../harness/cli.ts";

export const ACME = "https://github.com/acme/skills";
export const ACME_ID = "1c9d0b7e4a2f6d38";
export const ZETA = "https://gitlab.com/zeta/tools";
export const ZETA_ID = "7a3b5c9e1f2d4a60";
export const ACCOUNT = "ssh://git@github.com/me/account";
export const ACCOUNT_ID = "5e8a1c3b7d9f2a64";

const HOUR = 3_600_000;
const DAY = 24 * HOUR;
const ago = (ms: number) => new Date(Date.now() - ms).toISOString();

/** acme: pinned to v2, read-only here, fetched three days ago. */
export const acme: SourceEvent = {
  access: "read-only",
  commit: "3f2a9c1e8d7b6a",
  default_branch: "main",
  id: ACME_ID,
  last_fetched: ago(3 * DAY + 2 * HOUR),
  pin: "v2",
  schema_version: 2,
  type: "source",
  url: ACME,
};

/** The sources `source list` reports: acme, zeta never fetched, the account remote two hours ago. */
export const sources: SourceEvent[] = [
  acme,
  {
    access: "unknown",
    id: ZETA_ID,
    schema_version: 2,
    type: "source",
    url: ZETA,
  },
  {
    access: "writable",
    account: true,
    id: ACCOUNT_ID,
    last_fetched: ago(2 * HOUR),
    schema_version: 2,
    skills: 2,
    type: "source",
    url: ACCOUNT,
  },
];

const sourceSkill = (
  source: string,
  subpath: string,
  name: string,
  description: string
): SourceSkillEvent => ({
  description,
  name,
  schema_version: 2,
  source,
  subpath,
  tree: `t-${name}`,
  type: "source_skill",
});

/** acme's listing, sorted by subpath as the CLI gives it: four to install, four this machine holds. */
export const acmeSkills: SourceSkillEvent[] = [
  sourceSkill(ACME_ID, "skills/artifacts", "artifacts", "Build web artifacts"),
  sourceSkill(ACME_ID, "skills/docx", "docx", "Create and edit Word documents"),
  sourceSkill(ACME_ID, "skills/forms", "forms", "Fill in forms"),
  sourceSkill(ACME_ID, "skills/letters", "letters", "Write formal letters"),
  sourceSkill(ACME_ID, "skills/pdf", "pdf", "Read, merge and fill PDF files"),
  sourceSkill(ACME_ID, "skills/scratch", "scratch", "Scratch notes"),
  sourceSkill(ACME_ID, "skills/slides", "slides", "Build slide decks"),
  sourceSkill(ACME_ID, "skills/xlsx", "xlsx", "Read and write spreadsheets"),
];

/** Three configurations that take a placement, windsurf disabled, and two that read the library. */
export const configurations: Configuration[] = [
  configuration("claude-code", { name: "Claude Code" }),
  configuration("codex", { name: "Codex", reads_library: true }),
  configuration("cursor", { name: "Cursor" }),
  configuration("gemini-cli", { name: "Gemini CLI", reads_library: true }),
  configuration("windsurf", { enabled: false, name: "Windsurf" }),
];

const universal = ["codex", "gemini-cli"];

/**
 * The library: pdf installed from acme (Added), my-letters forked from acme's letters (Forked), an
 * unmanaged scratch (Already in library), and an xlsx from zeta (Name in use for acme's xlsx).
 */
export const library: LibraryEntry[] = [
  {
    base_hash: "h-letters",
    content_hash: "h-my-letters",
    fork_id: "f0f0f0f0f0f0",
    kind: "managed",
    name: "my-letters",
    placements: [],
    source: ACCOUNT,
    state: "current",
    universal,
    upstream: ACME,
    upstream_subpath: "skills/letters",
  },
  {
    base_hash: "h-pdf",
    content_hash: "h-pdf",
    kind: "managed",
    name: "pdf",
    placements: [],
    source: ACME,
    state: "current",
    subpath: "skills/pdf",
    universal,
  },
  {
    content_hash: "h-scratch",
    kind: "unmanaged",
    name: "scratch",
    placements: [],
    universal,
  },
  {
    base_hash: "h-xlsx",
    content_hash: "h-xlsx",
    kind: "managed",
    name: "xlsx",
    placements: [],
    source: ZETA,
    state: "current",
    subpath: "xlsx",
    universal,
  },
];

/** The serve child's first inventory on this machine. */
export const inventory = {
  configurations,
  library,
  scan_counter: 1,
  type: "inventory",
} satisfies InlineEvent;

/** Your skills on the account remote: one forked from acme's pdf, one made from scratch. */
export const installable: InstallableSkillEvent[] = [
  {
    base_hash: "h-pdf",
    commit: "4c2e9a1bbbb",
    fork_id: "a1a1a1a1a1a1",
    name: "my-pdf",
    schema_version: 2,
    source: ACCOUNT,
    type: "installable_skill",
    upstream: ACME,
    upstream_commit: "3f2a9c1e8d7b6a",
    upstream_subpath: "skills/pdf",
  },
  {
    commit: "9b41c07aaaa",
    fork_id: "b2b2b2b2b2b2",
    name: "release-notes",
    schema_version: 2,
    source: ACCOUNT,
    type: "installable_skill",
  },
];

/** What serve answers for "doc": one skill of each shared source, sorted by source URL then name. */
export const docResults: SearchResult[] = [
  {
    description: "Create and edit Word documents",
    name: "docx",
    source: ACME,
    subpath: "skills/docx",
    tree: "t-docx",
  },
  {
    description: "Write docs from code comments",
    name: "docgen",
    source: ZETA,
    subpath: "docgen",
    tree: "t-docgen",
  },
];
