// What the Export settings and Import settings sections of Settings read from a name, a path and
// the Sources slice: pure functions, so that the sections show only what the CLI reports.
import type { SourceEvent } from "#/cli/events.gen.ts";

/** The name Export starts with: the snapshot is written to the library as agentx-export.json. */
export const DEFAULT_SNAPSHOT_NAME = "agentx-export";

/**
 * Whether `export <name>` takes `name`, by the CLI's rule for a snapshot name: a file name with no
 * extension and no slash, which also rules out a hidden one, since its leading dot starts an
 * extension. A leading `-` would be read as a flag.
 */
export const isSnapshotName = (name: string) =>
  name !== "" && !/[./\\]/u.test(name) && !name.startsWith("-");

/** The last part of a path the open dialog returned: "agentx-export.json". */
export const fileName = (path: string) =>
  /[^/\\]+(?=[/\\]*$)/u.exec(path)?.[0] ?? path;

/**
 * The sources of the settings this machine has not fetched, as `source list` reports them: a
 * shared source whose event carries no `commit` (the account repo holds no ref for it), and an
 * account remote whose event carries no `skills` (this machine has not fetched it). An import
 * restores the settings entry of such a source and nothing of the source itself.
 */
export const notFetched = (sources: readonly SourceEvent[]) =>
  sources.filter((s) =>
    s.account === true ? s.skills === undefined : s.commit === undefined
  );

/**
 * The `source add` that adds `source` again as the settings hold it: the URL with `#<pin>` when it
 * is pinned, and `--account` for the account remote, which is a whole repository and has no pin.
 */
export const sourceAddArgs = (source: SourceEvent): string[] => {
  if (source.account === true) {
    return ["source", "add", source.url, "--account"];
  }
  const pin = source.pin ?? "";
  return ["source", "add", pin === "" ? source.url : `${source.url}#${pin}`];
};
