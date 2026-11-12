// The order the app lists agent clients in where it cannot show them all at once: the most used
// first. The rank is a judgement of real-world use, kept as a plain list so it is easy to edit.
import type { Configuration } from "#/cli/events.gen.ts";

/**
 * Configuration ids (the CLI's client slugs), the most used client first. A client left out
 * follows the ranked ones, by name. To change the order, move a line.
 */
const ranked = [
  "claude-code",
  "cursor",
  "codex",
  "github-copilot",
  "gemini-cli",
  "windsurf",
  "opencode",
  "cline",
  "roo",
  "kilo",
  "amp",
  "goose",
  "droid",
  "zed",
  "warp",
  "antigravity",
  "continue",
  "augment",
  "junie",
  "kiro-cli",
  "qwen-code",
  "trae",
  "crush",
  "openhands",
  "devin",
  "pi",
  "openclaw",
  "mistral-vibe",
  "grok",
  "qoder",
];

const rankOf = new Map(ranked.map((id, rank) => [id, rank]));

/** The rank of the client with this configuration id: 0 is the most used; unranked ids tie last. */
export const popularityOf = (id: string): number =>
  rankOf.get(id) ?? ranked.length;

/** Sorts configurations by popularity, then by name. */
export const byPopularity = (
  a: Pick<Configuration, "id" | "name">,
  b: Pick<Configuration, "id" | "name">
): number =>
  popularityOf(a.id) - popularityOf(b.id) || a.name.localeCompare(b.name);
