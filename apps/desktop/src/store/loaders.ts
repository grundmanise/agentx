// The slices that one-shot reads fill: the adoption preview, the store and what screens
// read on demand. This machine, its settings and its sources come with the inventory instead
// (src/store/feed.ts). The adoption preview loads once the startup checks pass, after an import,
// when Settings opens and on Check updates. A `skill check-updates` run sets when the last
// check ended; what it found reaches the inventory that follows it.
import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  DiffEvent,
  HistoryEvent,
  InstallableSkillEvent,
  SkillFileEvent,
  SourceSkillEvent,
} from "#/cli/events.gen.ts";
import { checksPass } from "#/startup/startup.ts";
import { cachedRead, keepRead } from "#/store/read-cache.ts";
import { agentx, onFinished } from "#/store/runner.ts";
import type { CommandOptions, Finished } from "#/store/runner.ts";
import { sourcesOf, useAppStore } from "#/store/store.ts";
import type { AppState, FilesRead } from "#/store/store.ts";

/** The URL of the hub, as the inventory lists the sources; undefined when none is set. */
const hubUrl = () =>
  sourcesOf(useAppStore.getState()).find((s) => s.hub === true)?.url;

/**
 * Whether a skill's version names its content: it starts with the content hash. A read of a
 * version without one is not kept for the next launch, since it names no content.
 */
const hashed = (version: string) => !version.startsWith(":");

/**
 * Runs a command once the startup checks pass. A screen that shows the kept inventory under the
 * frosted pane asks for its reads as it mounts: such a read waits here, however long the checks
 * take.
 */
const runWhenReady = async (
  args: readonly string[],
  options: CommandOptions
) => {
  await checksPass();
  return await agentx(args, options);
};

// Each read's latest run: a slower earlier run of the same read does not overwrite a later one.
const latest = new Map<string, number>();
const running = new Map<string, Promise<void>>();
let runs = 0;

const runLoad = async (
  args: string[],
  read: (events: CliEvent[], state: AppState) => Partial<AppState>,
  mine: number
) => {
  const key = args.join(" ");
  const outcome = await runWhenReady(args, {
    label: `load ${key}`,
    sideEffect: false,
  });
  if (latest.get(key) !== mine) {
    // A later run replaced this one: it ends with that run, so its caller finds the store filled.
    await running.get(key);
  } else if (outcome.exitCode === 0) {
    useAppStore.setState((state) => read(outcome.events, state));
  }
};

const load = async (
  args: string[],
  read: (events: CliEvent[], state: AppState) => Partial<AppState>
) => {
  const key = args.join(" ");
  runs += 1;
  latest.set(key, runs);
  const run = runLoad(args, read, runs);
  running.set(key, run);
  await run;
};

/** The label of the control that loads `args`, for `usePending`. */
export const loadLabel = (args: string[]) => `load ${args.join(" ")}`;

/**
 * Loads the store and its size: `machine`, whose `store` and `store_bytes` the inventory leaves
 * out. Settings calls it each time Storage shows.
 */
export const loadStore = async () => {
  await load(["machine"], (events) => {
    const event = events.find((e) => e.type === "machine");
    return event === undefined
      ? {}
      : { store: { store: event.store, store_bytes: event.store_bytes } };
  });
};

/** The commit the listing in the store was read at, by source id. */
const listedAt = new Map<string, string>();
/** The reads of a listing going on, by source id and the commit they read for. */
const listingReads = new Map<string, Promise<void>>();

const readListing = async (id: string, commit: string | undefined) => {
  if (commit !== undefined && listedAt.get(id) === commit) {
    return;
  }
  if (commit !== undefined) {
    const kept = await cachedRead<SourceSkillEvent[]>("listing", id, commit);
    if (kept !== undefined) {
      listedAt.set(id, commit);
      useAppStore.setState((s) => ({
        listings: { ...s.listings, [id]: kept },
      }));
      return;
    }
  }
  // Only what this run read is kept: a failed run leaves the listing as it was.
  let read: SourceSkillEvent[] | undefined;
  let at: string | undefined;
  await load(["source", "skills", id], (events, state) => {
    read = events.filter((e) => e.type === "source_skill");
    at = events.find((e) => e.type === "source")?.commit;
    return { listings: { ...state.listings, [id]: read } };
  });
  if (read !== undefined && at !== undefined) {
    listedAt.set(id, at);
    void keepRead("listing", id, at, read);
  }
};

/**
 * Loads the skills of a source, for its open pill: the listing kept for `commit`, the one the
 * inventory gives the source, else `source skills <id>`, a read of the last fetch. A listing the
 * store holds at that commit is read no more, so the list changes once per fetch that moves the
 * source.
 */
export const loadListing = async (id: string, commit?: string) => {
  const key = `${id}\u0000${commit ?? ""}`;
  const going = listingReads.get(key);
  if (going !== undefined) {
    await going;
    return;
  }
  const read = readListing(id, commit);
  listingReads.set(key, read);
  try {
    await read;
  } finally {
    listingReads.delete(key);
  }
};

/**
 * Loads your skills the hub holds that this machine has not installed:
 * `skill list --remote`, which fetches the hub. The Add screen calls it when the Your
 * skills pill opens. The list kept from an earlier launch for the same hub shows while
 * it runs.
 */
export const loadInstallable = async () => {
  const remote = hubUrl();
  if (
    remote !== undefined &&
    useAppStore.getState().installable === undefined
  ) {
    const kept = await cachedRead<InstallableSkillEvent[]>(
      "installable",
      remote,
      ""
    );
    if (
      kept !== undefined &&
      useAppStore.getState().installable === undefined
    ) {
      useAppStore.setState({ installable: kept });
    }
  }
  // Only what this run read is kept: a failed run leaves the list as it was.
  let read: InstallableSkillEvent[] | undefined;
  await load(["skill", "list", "--remote"], (events) => {
    read = events.filter((e) => e.type === "installable_skill");
    return { installable: read };
  });
  // The list belongs to the remote set when the command ran: kept only if that is still this one.
  const now = hubUrl();
  if (remote !== undefined && read !== undefined && now === remote) {
    void keepRead("installable", remote, "", read);
  }
};

// The preview's run, while one runs, and whether a write since it started asks for one more.
let adoptionRun: Promise<void> | undefined;
let adoptionStale = false;

/** Runs the preview until no write asked for another run while it ran. */
const readAdoption = async () => {
  try {
    do {
      adoptionStale = false;
      // oxlint-disable-next-line no-await-in-loop
      await load(["skill", "import"], (events) => ({
        adoption: events.filter((e) => e.type === "adoption"),
      }));
    } while (adoptionStale);
  } finally {
    adoptionRun = undefined;
  }
};

/**
 * Loads what `skill import` would take over, from its preview, which writes nothing. A call while
 * the preview runs joins that run; `afterWrite`, a call after an import, runs it once more after.
 */
export const loadAdoption = async ({ afterWrite = false } = {}) => {
  if (adoptionRun === undefined) {
    adoptionRun = readAdoption();
  } else {
    adoptionStale ||= afterWrite;
  }
  await adoptionRun;
};

// The version of each skill whose history was read, or is being read: name → version.
const historyVersions = new Map<string, string>();

/**
 * Loads the history of a hub skill: `skill history <name>`, a read. `version` names
 * what the history is of, such as the skill's content and state from the inventory: the same
 * version is read once, however often the skill is selected, and a change to it reads again.
 */
export const loadHistory = async (name: string, version: string) => {
  if (historyVersions.get(name) === version) {
    return;
  }
  historyVersions.set(name, version);
  // The entries of the version before, if any, are not this version's: they go first.
  useAppStore.setState((s) => {
    const { [name]: previous, ...rest } = s.histories;
    return previous === undefined || previous.version === version
      ? s
      : { histories: rest };
  });
  // A history kept from an earlier launch for this version shows while `skill history` runs again:
  // the version names the skill's content and state, not each commit, such as an amended message.
  const kept = hashed(version)
    ? await cachedRead<HistoryEvent[]>("history", name, version)
    : undefined;
  if (historyVersions.get(name) !== version) {
    return;
  }
  if (kept !== undefined) {
    useAppStore.setState((s) => ({
      histories: { ...s.histories, [name]: { entries: kept, version } },
    }));
  }
  const outcome = await runWhenReady(["skill", "history", name], {
    label: `history ${name}`,
    sideEffect: false,
  });
  if (historyVersions.get(name) !== version) {
    return;
  }
  if (outcome.exitCode === 0) {
    const entries = outcome.events.filter((e) => e.type === "history");
    useAppStore.setState((s) => ({
      histories: { ...s.histories, [name]: { entries, version } },
    }));
    if (hashed(version)) {
      void keepRead("history", name, version, entries);
    }
  } else {
    // A failed read is kept as such, unless a kept history shows, and tried again the next time
    // the skill is selected.
    if (kept === undefined) {
      useAppStore.setState((s) => ({
        histories: { ...s.histories, [name]: { failed: true, version } },
      }));
    }
    historyVersions.delete(name);
  }
};

// The version of each skill whose files were read, or are being read: row key → version.
const fileVersions = new Map<string, string>();

/** The reads that show a skill's files: `skill files` and, for a modified skill, `skill diff`. */
export interface FilesRequest {
  /** The arguments after `skill files`: the skill's name, as the CLI looks it up, and its flags. */
  args: string[];
  /** The skill whose unpublished edits `skill diff` reads; absent when the skill is not modified. */
  diff?: string;
}

/**
 * Loads the files of the skill `key` names: `skill files`, and `skill diff <name>` for a modified
 * skill, both reads, run at once. `version` names what the files are of, as for `loadHistory`:
 * the same version is read once, however often the skill is selected, and a change reads again.
 */
export const loadFiles = async (
  key: string,
  request: FilesRequest,
  version: string
) => {
  if (fileVersions.get(key) === version) {
    return;
  }
  fileVersions.set(key, version);
  useAppStore.setState((s) => {
    const { [key]: previous, ...rest } = s.files;
    return previous === undefined || previous.version === version
      ? s
      : { files: rest };
  });
  // The files kept from an earlier launch for this version are this version's.
  const kept = hashed(version)
    ? await cachedRead<FilesRead>("files", key, version)
    : undefined;
  if (fileVersions.get(key) !== version) {
    return;
  }
  if (kept !== undefined) {
    useAppStore.setState((s) => ({ files: { ...s.files, [key]: kept } }));
    return;
  }
  const [files, diff] = await Promise.all([
    runWhenReady(["skill", "files", ...request.args], {
      label: `files ${key}`,
      sideEffect: false,
    }),
    request.diff === undefined
      ? undefined
      : runWhenReady(["skill", "diff", request.diff], {
          label: `diff ${request.diff}`,
          sideEffect: false,
        }),
  ]);
  if (fileVersions.get(key) !== version) {
    return;
  }
  if (files.exitCode !== 0) {
    useAppStore.setState((s) => ({
      files: { ...s.files, [key]: { failed: true, version } },
    }));
    fileVersions.delete(key);
    return;
  }
  const read: FilesRead = {
    files: files.events.filter((e) => e.type === "skill_file"),
    version,
  };
  if (diff !== undefined) {
    if (diff.exitCode === 0) {
      read.diffs = diff.events.filter((e) => e.type === "diff");
    } else {
      // The files are shown without their edits, and both are read again on the next selection.
      read.diffFailed = true;
      fileVersions.delete(key);
    }
  }
  useAppStore.setState((s) => ({ files: { ...s.files, [key]: read } }));
  if (hashed(version) && read.diffFailed !== true) {
    void keepRead("files", key, version, read);
  }
};

// The version of each skill of a source whose SKILL.md was read, or is being read: card key →
// version.
const skillMdVersions = new Map<string, string>();

/** What `source read` reads: the source's id or URL, the skill's subpath and the tree listed. */
export interface SkillMdRequest {
  source: string;
  /** "" for a skill at the root, which `source read` takes as ".". */
  subpath: string;
  tree: string;
}

/**
 * Loads the SKILL.md of a skill of a source, before it is installed: `source read <source>
 * <subpath>`, a read of the last fetch. `key` names the skill on the Sources screen. The same
 * subpath and tree are read once, however often the skill's row opens, and a fetch that changes
 * the tree reads again. A failed read is kept as such, and tried again the next time the row opens.
 */
export const loadSkillMd = async (key: string, request: SkillMdRequest) => {
  const version = `${request.subpath}\u0000${request.tree}`;
  if (skillMdVersions.get(key) === version) {
    return;
  }
  skillMdVersions.set(key, version);
  useAppStore.setState((s) => {
    const { [key]: previous, ...rest } = s.skillMds;
    return previous === undefined || previous.version === version
      ? s
      : { skillMds: rest };
  });
  // The SKILL.md kept from an earlier launch for this tree is this tree's.
  const kept =
    request.tree === ""
      ? undefined
      : await cachedRead<SkillFileEvent>("skill-md", key, version);
  if (skillMdVersions.get(key) !== version) {
    return;
  }
  if (kept !== undefined) {
    useAppStore.setState((s) => ({
      skillMds: { ...s.skillMds, [key]: { file: kept, version } },
    }));
    return;
  }
  const subpath = request.subpath === "" ? "." : request.subpath;
  const outcome = await runWhenReady(
    ["source", "read", request.source, subpath],
    {
      label: `read ${request.source} ${subpath}`,
      // The row says the file could not be read; a toast per row opened would add nothing.
      ownFailure: true,
      sideEffect: false,
    }
  );
  if (skillMdVersions.get(key) !== version) {
    return;
  }
  const file = outcome.events.find((e) => e.type === "skill_file");
  if (outcome.exitCode !== 0 || file === undefined) {
    useAppStore.setState((s) => ({
      skillMds: { ...s.skillMds, [key]: { failed: true, version } },
    }));
    skillMdVersions.delete(key);
    return;
  }
  useAppStore.setState((s) => ({
    skillMds: { ...s.skillMds, [key]: { file, version } },
  }));
  if (request.tree !== "") {
    void keepRead("skill-md", key, version, file);
  }
};

// The versions of each update whose changes were read, or are being read: name → updateKey.
const updateDiffCandidates = new Map<string, string>();

/** The label of the read of what the update of `name` changes, for `usePending`. */
export const updateDiffLabel = (name: string) => `update diff ${name}`;

/**
 * Loads what the update of `name` changes: `skill diff <name> --update`, what `skill update <name>`
 * would apply, for a hub skill what the hub holds, else its upstream's update. `candidate` names
 * the versions the inventory offers, the base among them (`updateKey`): the same update is read
 * once, however often its changes are shown, and one of other versions reads again. A failed read
 * is kept with the CLI's message, and tried again the next time the changes are shown.
 */
export const loadUpdateDiff = async (name: string, candidate: string) => {
  if (updateDiffCandidates.get(name) === candidate) {
    return;
  }
  updateDiffCandidates.set(name, candidate);
  useAppStore.setState((s) => {
    const { [name]: previous, ...rest } = s.updateDiffs;
    return previous === undefined || previous.candidate === candidate
      ? s
      : { updateDiffs: rest };
  });
  // The changes kept from an earlier launch of these versions are this update's.
  const kept = await cachedRead<DiffEvent[]>("update-diff", name, candidate);
  if (updateDiffCandidates.get(name) !== candidate) {
    return;
  }
  if (kept !== undefined) {
    useAppStore.setState((s) => ({
      updateDiffs: { ...s.updateDiffs, [name]: { candidate, diffs: kept } },
    }));
    return;
  }
  const outcome = await runWhenReady(["skill", "diff", name, "--update"], {
    label: updateDiffLabel(name),
    ownFailure: true,
    sideEffect: false,
  });
  if (updateDiffCandidates.get(name) !== candidate) {
    return;
  }
  if (outcome.exitCode === 0) {
    const diffs = outcome.events.filter((e) => e.type === "diff");
    useAppStore.setState((s) => ({
      updateDiffs: { ...s.updateDiffs, [name]: { candidate, diffs } },
    }));
    void keepRead("update-diff", name, candidate, diffs);
    return;
  }
  const error = outcome.events.find((e) => e.type === "error");
  const problem =
    error === undefined
      ? { message: "agentx could not read the changes" }
      : {
          message: error.message,
          ...(error.hint === undefined ? {} : { hint: error.hint }),
        };
  useAppStore.setState((s) => ({
    updateDiffs: {
      ...s.updateDiffs,
      [name]: { candidate, failed: true, problem },
    },
  }));
  updateDiffCandidates.delete(name);
};

/**
 * Loads the adoption preview again after a command that may have changed it. The preview is read
 * on demand, not on each scan: what another tool installed seldom changes. An import changes it,
 * and Check updates is the Skills screen's refresh.
 */
const reloadAdoption = (command: string, args: readonly string[]) => {
  if (command === "skill import" && args.length > 2) {
    void loadAdoption({ afterWrite: true });
  }
  if (command === "skill check-updates") {
    void loadAdoption();
  }
};

/** What to load again after a command that may have changed something. */
const afterCommand = ({ args, command, exitCode, read }: Finished) => {
  if (read) {
    return;
  }
  reloadAdoption(command, args);
  // A check that could not reach a source (exit 3) still checked the others.
  if (
    command === "skill check-updates" &&
    (exitCode === EXIT_CODES.ok || exitCode === EXIT_CODES.source)
  ) {
    useAppStore.setState({ lastChecked: new Date().toISOString() });
  }
};

let started = false;

/** Loads every slice, and keeps each one current from now on. Called once the checks pass. */
export const startLoaders = () => {
  if (started) {
    return;
  }
  started = true;
  onFinished(afterCommand);
  void loadAdoption();
};
