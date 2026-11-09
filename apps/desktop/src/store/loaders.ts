// The slices that one-shot reads fill: settings, sources and the adoption preview. Each loads
// once the startup checks pass, and again after the commands that change it. The update check of
// `skill check-updates` replaces the update candidates serve found, and `skill update` takes away
// those of the skills it updated.
import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { CliEvent } from "#/cli/events.gen.ts";
import { onFinished, runCommand } from "#/store/runner.ts";
import type { Finished } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";
import type { AppState, FilesRead } from "#/store/store.ts";

// Each read's latest run: a slower earlier run of the same read does not overwrite a later one.
const latest = new Map<string, number>();
let runs = 0;

const load = async (
  args: string[],
  read: (events: CliEvent[], state: AppState) => Partial<AppState>
) => {
  const key = args.join(" ");
  runs += 1;
  const mine = runs;
  latest.set(key, mine);
  const outcome = await runCommand(args, { label: `load ${key}` });
  if (outcome.exitCode === 0 && latest.get(key) === mine) {
    useAppStore.setState((state) => read(outcome.events, state));
  }
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
 * calls it when a source's pill opens, and again after it fetches the source.
 */
export const loadListing = async (id: string) => {
  await load(["source", "skills", id], (events, state) => ({
    listings: {
      ...state.listings,
      [id]: events.filter((e) => e.type === "source_skill"),
    },
  }));
};

/**
 * Loads your skills the account remote holds that this machine has not installed:
 * `skill list --remote`, which fetches the account remote. The Add screen calls it when the Your
 * skills pill opens.
 */
export const loadInstallable = async () => {
  await load(["skill", "list", "--remote"], (events) => ({
    installable: events.filter((e) => e.type === "installable_skill"),
  }));
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
  const outcome = await runCommand(["skill", "history", name], {
    label: `history ${name}`,
  });
  if (historyVersions.get(name) !== version) {
    return;
  }
  if (outcome.exitCode === 0) {
    useAppStore.setState((s) => ({
      histories: {
        ...s.histories,
        [name]: {
          entries: outcome.events.filter((e) => e.type === "history"),
          version,
        },
      },
    }));
  } else {
    // A failed read is kept as such, and tried again the next time the skill is selected.
    useAppStore.setState((s) => ({
      histories: { ...s.histories, [name]: { failed: true, version } },
    }));
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
  const [files, diff] = await Promise.all([
    runCommand(["skill", "files", ...request.args], { label: `files ${key}` }),
    request.diff === undefined
      ? undefined
      : runCommand(["skill", "diff", request.diff], {
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
      updates: Object.fromEntries(
        events
          .filter((e) => e.type === "update_available")
          .map((e) => [e.name, e])
      ),
    });
  }
  // The skills an update applied to, which its `library_skill` events name, have no update left.
  // An aborted merge emits `library_skill` too, but its update is still offered.
  if (command === "skill update" && !args.includes("--abort")) {
    const updated = new Set(
      events.filter((e) => e.type === "library_skill").map((e) => e.name)
    );
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
