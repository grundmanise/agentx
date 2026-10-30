// The DETAILS of a skill, per kind: key and value pairs read from its library entry, its node and
// the plugin that provides it, as the CLI reported them.
import type { Configuration, LibraryEntry } from "#/cli/events.gen.ts";
import { shortId } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

export interface Detail {
  key: string;
  value: string;
}

const at = (source: string, subpath: string | undefined) =>
  subpath === undefined || subpath === "" ? source : `${source}/${subpath}`;

/** The directory one of your own skills is checked out in, as the library occurrence resolves. */
const filesOf = (row: SkillRow): string | undefined => {
  const library = row.entry?.placements.find((p) => p.mode === "library");
  const occurrence =
    row.occurrences.find(
      (o) =>
        library !== undefined &&
        o.configuration === library.configuration &&
        o.path === library.path
    ) ?? row.occurrences[0];
  return occurrence?.resolved_path;
};

const sourceDetails = (entry: LibraryEntry): Detail[] => {
  const details: Detail[] = [
    { key: "Source", value: at(entry.source ?? "", entry.subpath) },
  ];
  if (entry.upstream_commit !== undefined) {
    details.push({ key: "Version", value: shortId(entry.upstream_commit) });
  }
  if (entry.candidate !== undefined) {
    details.push({
      key: "Update",
      value: shortId(entry.candidate.upstream_commit),
    });
  }
  if (entry.state !== undefined) {
    details.push({ key: "State", value: entry.state });
  }
  return details;
};

const yoursDetails = (row: SkillRow, entry: LibraryEntry): Detail[] => {
  const details: Detail[] = [];
  if (entry.upstream !== undefined) {
    details.push({
      key: "Forked from",
      value: at(entry.upstream, entry.upstream_subpath),
    });
    if (entry.upstream_commit !== undefined) {
      details.push({ key: "Based on", value: shortId(entry.upstream_commit) });
    }
  }
  details.push({
    key: "Published to",
    value: entry.source ?? "No account remote",
  });
  const files = filesOf(row);
  if (files !== undefined) {
    details.push({ key: "Files", value: files });
  }
  if (entry.fork_id !== undefined) {
    details.push({ key: "Id", value: shortId(entry.fork_id) });
  }
  return details;
};

const pluginDetails = (
  row: SkillRow,
  configurations: Configuration[]
): Detail[] => {
  const { plugin } = row;
  const [first] = row.occurrences;
  if (plugin === undefined) {
    return [{ key: "Plugin", value: first?.plugin ?? "" }];
  }
  const details: Detail[] = [
    { key: "Plugin", value: `${plugin.name} ${plugin.version}`.trim() },
  ];
  if (plugin.marketplace !== "") {
    details.push({ key: "Marketplace", value: plugin.marketplace });
  }
  const agent = configurations.find((c) => c.id === plugin.configuration);
  details.push({ key: "Agent", value: agent?.name ?? plugin.configuration });
  return details;
};

const diskDetails = (row: SkillRow): Detail[] => {
  const paths =
    row.occurrences.length > 0
      ? row.occurrences.map((o) => o.path)
      : (row.entry?.placements.map((p) => p.path) ?? []);
  // Universal clients share the library directory: one line per place, not per client.
  const details: Detail[] = [...new Set(paths)].map((path) => ({
    key: "Found at",
    value: path,
  }));
  if (row.adoption !== undefined) {
    details.push({
      key: "Installed by",
      value:
        row.adoption.source === undefined
          ? "skills CLI"
          : `skills CLI · ${row.adoption.source}`,
    });
  }
  return details;
};

/** The DETAILS of `row`; `configurations` name the agent client a plugin is installed in. */
export const detailsOf = (
  row: SkillRow,
  configurations: Configuration[]
): Detail[] => {
  const { entry } = row;
  switch (row.kind) {
    case "source": {
      return entry === undefined ? [] : sourceDetails(entry);
    }
    case "yours": {
      return entry === undefined ? [] : yoursDetails(row, entry);
    }
    case "plugin": {
      return pluginDetails(row, configurations);
    }
    case "disk": {
      return diskDetails(row);
    }
    default: {
      return [];
    }
  }
};
