// The DETAILS of a skill, per kind: key and value pairs read from its library entry, its node and
// the plugin that provides it, as the CLI reported them. An https URL opens in the browser, and a
// directory on this machine shows in Finder.
import type { Configuration, LibraryEntry } from "#/cli/events.gen.ts";
import { shortId, toolName } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { skillPage } from "#/screens/sources/projection.ts";

/** Where a value leads: a URL opened in the browser, or a path shown in Finder. */
export type DetailLink =
  | { kind: "page"; url: string }
  | { kind: "path"; path: string };

export interface Detail {
  key: string;
  value: string;
  link?: DetailLink;
}

/** `url` opened in the browser, when it is an https URL; an SSH remote stays text. */
const pageLink = (url: string | undefined): DetailLink | undefined =>
  url?.startsWith("https://") === true ? { kind: "page", url } : undefined;

/**
 * The page the origin beside the skill's name opens: its source's, or the repository it was forked
 * from. Other skills have none.
 */
export const originPage = (row: SkillRow): string | undefined => {
  let url: string | undefined;
  if (row.kind === "source") {
    url = row.entry?.source;
  } else if (row.kind === "yours") {
    url = row.entry?.upstream;
  }
  return url?.startsWith("https://") === true ? url : undefined;
};

/** `path` shown in Finder, when it is absolute. */
const pathLink = (path: string): DetailLink | undefined =>
  path.startsWith("/") ? { kind: "path", path } : undefined;

/** `detail` with `link` when there is one. */
const linked = (detail: Detail, link: DetailLink | undefined): Detail =>
  link === undefined ? detail : { ...detail, link };

const at = (source: string, subpath: string | undefined) =>
  subpath === undefined || subpath === "" ? source : `${source}/${subpath}`;

/** The directory a hub skill is checked out in, as the library occurrence resolves. */
const worktreeOf = (row: SkillRow): string | undefined => {
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
    linked(
      { key: "Source", value: at(entry.source ?? "", entry.subpath) },
      pageLink(entry.source)
    ),
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
    // The skill's directory at the commit it was forked from, as the fork's name in HISTORY opens;
    // the repository when the host's pages are unknown.
    const forkedAt =
      entry.upstream_commit === undefined
        ? undefined
        : skillPage(
            entry.upstream,
            entry.upstream_commit,
            entry.upstream_subpath ?? ""
          );
    details.push(
      linked(
        {
          key: "Forked from",
          value: at(entry.upstream, entry.upstream_subpath),
        },
        pageLink(forkedAt ?? entry.upstream)
      )
    );
  }
  details.push(
    linked(
      { key: "Published to", value: entry.source ?? "Not published" },
      pageLink(entry.source)
    )
  );
  const files = worktreeOf(row);
  if (files !== undefined) {
    details.push(linked({ key: "Files", value: files }, pathLink(files)));
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
  const details: Detail[] = [...new Set(paths)].map((path) =>
    linked({ key: "Found at", value: path }, pathLink(path))
  );
  if (row.adoption !== undefined) {
    details.push({
      key: "Installed by",
      value:
        row.adoption.source === undefined
          ? toolName(row.adoption)
          : `${toolName(row.adoption)} · ${row.adoption.source}`,
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
