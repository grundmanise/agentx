// The slices that one-shot reads fill: the adoption preview, the store and what screens
// read on demand. This machine, its settings and its sources come with the inventory instead
// (src/store/feed.ts). The adoption preview loads once the startup checks pass, after an import,
// when Settings opens and on Check updates. A `skill check-updates` run sets when the last
// check ended; what it found reaches the inventory that follows it.
import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  InstallableSkillEvent,
  SkillFileEvent,
  SourceSkillEvent,
} from "#/cli/events.gen.ts";
import { checksPass } from "#/startup/startup.ts";
import { cachedRead, keepRead } from "#/store/read-cache.ts";
import { agentx, onFinished } from "#/store/runner.ts";
import type { CommandOptions, Finished } from "#/store/runner.ts";
import { sourcesOf, useAppStore } from "#/store/store.ts";
import type { AppState, SkillRead } from "#/store/store.ts";

/** The URL of the hub, as the inventory lists the sources; undefined when none is set. */
const hubUrl = () =>
  sourcesOf(useAppStore.getState()).find((s) => s.hub === true)?.url;

/**
 * Whether a skill's version names its content: it starts with the content hash. A read of a
 * version without one is not kept for the next launch, since it names no content.
 */
const hashed = (version: string) => version !== "" && !version.startsWith(":");

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

// The version of each skill that was read, or is being read: row key → version.
const skillVersions = new Map<string, string>();

/** What `skill show` emitted, as the store keeps it for `version`. */
const skillReadOf = (events: CliEvent[], version: string): SkillRead => {
  const diffs = events.filter((e) => e.type === "diff");
  return {
    edits: diffs.filter((d) => d.update !== true),
    files: events.filter((e) => e.type === "skill_file"),
    history: events.filter((e) => e.type === "history"),
    update: diffs.filter((d) => d.update === true),
    version,
  };
};

/**
 * Loads all the detail pane shows of a skill beyond the inventory, in one read: `skill show`,
 * whose events are the skill's files, its unpublished edits, what its update changes and its
 * history. `key` is the row's key and `args` the arguments after `skill show`: the skill's name,
 * as the CLI looks it up, and its flags. `version` names what the read is of, the skill's content
 * and state from the inventory: the same version is read once, however often the skill is
 * selected, and a change to it reads again. The read of the version before stays in the store
 * until this one replaces it, in one write, so every section that shows it changes at once. A
 * failed read is kept as such, with the CLI's message, and tried again the next time the skill is
 * selected.
 */
export const loadSkill = async (
  key: string,
  args: readonly string[],
  version: string
) => {
  if (skillVersions.get(key) === version) {
    return;
  }
  skillVersions.set(key, version);
  // A read kept from an earlier launch for this version shows while `skill show` runs again: the
  // version names the skill's content and state, not each commit, such as an amended message.
  const kept = hashed(version)
    ? await cachedRead<SkillRead>("skill", key, version)
    : undefined;
  if (skillVersions.get(key) !== version) {
    return;
  }
  if (kept !== undefined) {
    useAppStore.setState((s) => ({
      skillReads: { ...s.skillReads, [key]: kept },
    }));
  }
  const outcome = await runWhenReady(["skill", "show", ...args], {
    label: `show ${key}`,
    sideEffect: false,
  });
  if (skillVersions.get(key) !== version) {
    return;
  }
  if (outcome.exitCode !== 0) {
    skillVersions.delete(key);
    // A kept read stays: it is this version's.
    if (kept === undefined) {
      const error = outcome.events.find((e) => e.type === "error");
      const problem = {
        message: error?.message ?? "agentx could not read the skill",
      };
      useAppStore.setState((s) => ({
        skillReads: {
          ...s.skillReads,
          [key]: { failed: true, problem, version },
        },
      }));
    }
    return;
  }
  const read = skillReadOf(outcome.events, version);
  // A read that says what the kept one says changes nothing on the screen, and is not written.
  if (kept === undefined || JSON.stringify(kept) !== JSON.stringify(read)) {
    useAppStore.setState((s) => ({
      skillReads: { ...s.skillReads, [key]: read },
    }));
    if (hashed(version)) {
      void keepRead("skill", key, version, read);
    }
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
