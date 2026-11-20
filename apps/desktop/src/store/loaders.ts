// The slices that one-shot reads fill: settings, sources and the adoption preview. Each loads
// once the startup checks pass, and again after the commands that change it. The update check of
// `skill check-updates` replaces the update candidates serve found and sets when the last check
// ended, and `skill update` takes away those of the skills it updated.
import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  DiffEvent,
  HistoryEvent,
  InstallableSkillEvent,
  SkillFileEvent,
  SourceSkillEvent,
} from "#/cli/events.gen.ts";
import { cachedRead, keepRead } from "#/store/read-cache.ts";
import { onFinished, readCommand } from "#/store/runner.ts";
import type { Finished } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";
import type { AppState, FilesRead } from "#/store/store.ts";

/**
 * Whether a skill's version names its content: it starts with the content hash. A read of a
 * version without one is not kept for the next launch, since it names no content.
 */
const hashed = (version: string) => !version.startsWith(":");

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
  const outcome = await readCommand(args, { label: `load ${key}` });
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

/** Loads the machine settings: `config list`. */
export const loadSettings = async () => {
  await load(["config", "list"], (events) => {
    const event = events.find((e) => e.type === "settings");
    return event === undefined ? {} : { settings: event.settings };
  });
};

/** Loads this machine and where agentx keeps its files: `machine`. */
export const loadMachine = async () => {
  await load(["machine"], (events) => {
    const event = events.find((e) => e.type === "machine");
    return event === undefined ? {} : { machine: event };
  });
};

/** Loads the added sources: `source list`. The Add screen calls it when it opens. */
export const loadSources = async () => {
  await load(["source", "list"], (events) => ({
    sources: events.filter((e) => e.type === "source"),
  }));
};

/**
 * Loads the skills of a source: `source skills <id>`, a read of the last fetch. The Add screen
 * calls it when a source's pill opens, and again after it fetches the source. The listing kept
 * from an earlier launch shows while it runs.
 */
export const loadListing = async (id: string) => {
  if (useAppStore.getState().listings[id] === undefined) {
    const kept = await cachedRead<SourceSkillEvent[]>("listing", id, "");
    if (
      kept !== undefined &&
      useAppStore.getState().listings[id] === undefined
    ) {
      useAppStore.setState((s) => ({
        listings: { ...s.listings, [id]: kept },
      }));
    }
  }
  // Only what this run read is kept: a failed run leaves the listing as it was.
  let read: SourceSkillEvent[] | undefined;
  await load(["source", "skills", id], (events, state) => {
    read = events.filter((e) => e.type === "source_skill");
    return { listings: { ...state.listings, [id]: read } };
  });
  if (read !== undefined) {
    void keepRead("listing", id, "", read);
  }
};

/**
 * Loads your skills the account remote holds that this machine has not installed:
 * `skill list --remote`, which fetches the account remote. The Add screen calls it when the Your
 * skills pill opens. The list kept from an earlier launch for the same account remote shows while
 * it runs.
 */
export const loadInstallable = async () => {
  const remote = useAppStore
    .getState()
    .settings?.sources.find((s) => s.account === true)?.url;
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
  const now = useAppStore
    .getState()
    .settings?.sources.find((s) => s.account === true)?.url;
  if (remote !== undefined && read !== undefined && now === remote) {
    void keepRead("installable", remote, "", read);
  }
};

/** Loads what `adopt` would take over, from its preview, which writes nothing. */
export const loadAdoption = async () => {
  await load(["adopt"], (events) => ({
    adoption: events.filter((e) => e.type === "adoption"),
  }));
};

// The version of each skill whose history was read, or is being read: name → version.
const historyVersions = new Map<string, string>();

/**
 * Loads the history of one of your own skills: `skill history <name>`, a read. `version` names
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
  const outcome = await readCommand(["skill", "history", name], {
    label: `history ${name}`,
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
    readCommand(["skill", "files", ...request.args], { label: `files ${key}` }),
    request.diff === undefined
      ? undefined
      : readCommand(["skill", "diff", request.diff], {
          label: `diff ${request.diff}`,
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
  const outcome = await readCommand(
    ["source", "read", request.source, subpath],
    {
      label: `read ${request.source} ${subpath}`,
      // The row says the file could not be read; a toast per row opened would add nothing.
      ownFailure: true,
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

// The candidate of each update whose changes were read, or are being read: name → candidate.
const updateDiffCandidates = new Map<string, string>();

/** The label of the read of what the update of `name` changes, for `usePending`. */
export const updateDiffLabel = (name: string) => `update diff ${name}`;

/**
 * Loads what the update of `name` changes: `skill diff <name> --update`, a read of the two
 * versions the last update check pinned. `candidate` names the update: the same one is read once,
 * however often its changes are opened, and a newer one reads again. A failed read is kept with
 * the CLI's message, and tried again the next time the changes are opened.
 */
export const loadUpdateDiff = async (
  name: string,
  candidate: string,
  base: string
) => {
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
  // The changes kept from an earlier launch from this base to this candidate are this update's.
  const kept = await cachedRead<DiffEvent[]>(
    "update-diff",
    name,
    `${base}:${candidate}`
  );
  if (updateDiffCandidates.get(name) !== candidate) {
    return;
  }
  if (kept !== undefined) {
    useAppStore.setState((s) => ({
      updateDiffs: { ...s.updateDiffs, [name]: { candidate, diffs: kept } },
    }));
    return;
  }
  const outcome = await readCommand(["skill", "diff", name, "--update"], {
    label: updateDiffLabel(name),
    ownFailure: true,
  });
  if (updateDiffCandidates.get(name) !== candidate) {
    return;
  }
  if (outcome.exitCode === 0) {
    const diffs = outcome.events.filter((e) => e.type === "diff");
    useAppStore.setState((s) => ({
      updateDiffs: { ...s.updateDiffs, [name]: { candidate, diffs } },
    }));
    void keepRead("update-diff", name, `${base}:${candidate}`, diffs);
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

/** What to load again after a command that may have changed something. */
const afterCommand = ({ args, command, events, exitCode, read }: Finished) => {
  if (read) {
    return;
  }
  if (command.startsWith("config ") || command === "import") {
    void loadSettings();
  }
  if (command.startsWith("machine ")) {
    void loadMachine();
  }
  // The label is the machine's: Settings shows it from `machine`, which reads it again.
  if (command === "config set" && args[2] === "label") {
    void loadMachine();
  }
  if (
    command.startsWith("source ") ||
    command === "skill add" ||
    (command === "adopt" && args.length > 1)
  ) {
    void loadSources();
  }
  // A check that could not reach a source (exit 3) still lists what it found elsewhere.
  if (
    command === "skill check-updates" &&
    (exitCode === EXIT_CODES.ok || exitCode === EXIT_CODES.source)
  ) {
    useAppStore.setState({
      lastChecked: new Date().toISOString(),
      updates: Object.fromEntries(
        events
          .filter((e) => e.type === "update_available")
          .map((e) => [e.name, e])
      ),
    });
  }
  // The skills an update applied to, which its `library_skill` events name, have no update left,
  // and neither has the one skill an update of one name left current, which it names in none.
  // An aborted merge emits `library_skill` too, but its update is still offered.
  if (command === "skill update" && !args.includes("--abort")) {
    const updated = new Set(
      events.filter((e) => e.type === "library_skill").map((e) => e.name)
    );
    const one = args.at(2);
    if (
      exitCode === EXIT_CODES.ok &&
      one !== undefined &&
      !one.startsWith("-")
    ) {
      updated.add(one);
    }
    if (updated.size > 0) {
      useAppStore.setState((s) => ({
        updates: Object.fromEntries(
          Object.entries(s.updates).filter(([name]) => !updated.has(name))
        ),
      }));
    }
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
  // A change to the library can change what `adopt` would take over.
  useAppStore.subscribe((s, previous) => {
    const before = previous.inventory?.library;
    const now = s.inventory?.library;
    if (
      before !== undefined &&
      now !== undefined &&
      before !== now &&
      JSON.stringify(before) !== JSON.stringify(now)
    ) {
      void loadAdoption();
    }
  });
  void loadSettings();
  void loadSources();
  void loadAdoption();
  void loadMachine();
};
