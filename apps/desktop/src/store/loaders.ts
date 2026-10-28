// The slices that one-shot reads fill: settings, sources and the adoption preview. Each loads
// once the startup checks pass, and again after the commands that change it. The update check of
// `skill check-updates` replaces the update candidates serve found.
import type { CliEvent } from "#/cli/events.gen.ts";
import { onFinished, runCommand } from "#/store/runner.ts";
import type { Finished } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";
import type { AppState } from "#/store/store.ts";

// Each read's latest run: a slower earlier run of the same read does not overwrite a later one.
const latest = new Map<string, number>();
let runs = 0;

const load = async (
  args: string[],
  read: (events: CliEvent[]) => Partial<AppState>
) => {
  const key = args.join(" ");
  runs += 1;
  const mine = runs;
  latest.set(key, mine);
  const outcome = await runCommand(args, { label: `load ${key}` });
  if (outcome.exitCode === 0 && latest.get(key) === mine) {
    useAppStore.setState(read(outcome.events));
  }
};

/** Loads the machine settings: `config list`. */
export const loadSettings = async () => {
  await load(["config", "list"], (events) => {
    const event = events.find((e) => e.type === "settings");
    return event === undefined ? {} : { settings: event.settings };
  });
};

/** Loads the added sources: `source list`. The Add screen calls it when it opens. */
export const loadSources = async () => {
  await load(["source", "list"], (events) => ({
    sources: events.filter((e) => e.type === "source"),
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
  const outcome = await runCommand(["skill", "history", name], {
    label: `history ${name}`,
  });
  if (outcome.exitCode === 0) {
    if (historyVersions.get(name) === version) {
      useAppStore.setState((s) => ({
        histories: {
          ...s.histories,
          [name]: outcome.events.filter((e) => e.type === "history"),
        },
      }));
    }
  } else if (historyVersions.get(name) === version) {
    // A failed read is tried again the next time the skill is selected.
    historyVersions.delete(name);
  }
};

/** What to load again after a command that may have changed something. */
const afterCommand = ({ args, command, events, exitCode, read }: Finished) => {
  if (read) {
    return;
  }
  if (command.startsWith("config ") || command === "import") {
    void loadSettings();
  }
  if (
    command.startsWith("source ") ||
    command === "skill add" ||
    (command === "adopt" && args.length > 1)
  ) {
    void loadSources();
  }
  if (command === "skill check-updates" && exitCode === 0) {
    useAppStore.setState({
      updates: Object.fromEntries(
        events
          .filter((e) => e.type === "update_available")
          .map((e) => [e.name, e])
      ),
    });
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
};
