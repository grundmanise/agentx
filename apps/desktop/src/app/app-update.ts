// The app's own updates (ADR 0019), which the Rust side's updater finds and installs: it reads
// the update manifest of the app's channel. After the startup checks pass, and then once a day,
// the app checks for an update unless Automatic update checks is off in Settings, whose Check for
// updates checks at once. An update the check finds shows in the title bar
// (`app-update-status.tsx`), whose click installs it: the download's progress runs on its rim,
// and once it is installed a click restarts the app into the new version. A browser has no app to
// update, so none of this runs there.
import { invoke } from "@tauri-apps/api/core";
import { listen } from "@tauri-apps/api/event";
import { create } from "zustand";

import type { RimRun } from "#/components/progress-rim/rim-progress.ts";
import { notify, notifyFailure } from "#/components/toast.tsx";
import { inApp } from "#/lib/platform.ts";
import { readPreferences, savePreferences } from "#/lib/preferences.ts";

/** How often the app checks again while it runs: once a day. */
const CHECK_EVERY_MS = 24 * 60 * 60 * 1000;

/** An update the check found. */
interface Available {
  version: string;
  current_version: string;
}

/** The download's progress, in bytes, as the Rust side sends it while an update installs. */
interface Progress {
  downloaded: number;
  total: number | null;
}

/** Where the update stands: found, installing, or installed and waiting for a restart. */
export type AppUpdate =
  | { phase: "available"; version: string }
  | { phase: "installing"; version: string; percent?: number }
  | { phase: "installed"; version: string };

interface AppUpdateState {
  update?: AppUpdate;
  /** The install the rim shows: while it runs, then until the rim has faded. */
  rim?: RimRun;
  /** Whether Check for updates in Settings runs. */
  checking: boolean;
  /** When the last check that reached the manifest ended (Date.now()); undefined before one. */
  checkedAt?: number;
  /** The Check for updates preference; undefined until it is read. */
  checkUpdates?: boolean;
}

const useAppUpdateStore = create<AppUpdateState>()(() => ({
  checking: false,
}));

/** Where the update stands, for the title bar; undefined when there is none. */
export const useAppUpdate = () => useAppUpdateStore((s) => s.update);

/** The install the title bar's rim shows. */
export const useAppUpdateRim = () => useAppUpdateStore((s) => s.rim);

/** The rim has faded. */
export const settleAppUpdateRim = () => {
  useAppUpdateStore.setState({ rim: undefined });
};

/** When the last check that reached the manifest ended; undefined before one. */
export const useAppUpdateCheckedAt = () =>
  useAppUpdateStore((s) => s.checkedAt);

/** Whether Check for updates in Settings runs. */
export const useAppUpdateChecking = () => useAppUpdateStore((s) => s.checking);

/** The Check for updates preference; undefined until it is read. */
export const useCheckUpdates = () => useAppUpdateStore((s) => s.checkUpdates);

/**
 * Asks the Rust side for an update. An update already installing or installed stays as it is.
 * With `manual`, a toast says when there is none or the check failed; a background check is
 * silent.
 */
const check = async (manual: boolean) => {
  const { update } = useAppUpdateStore.getState();
  if (update !== undefined && update.phase !== "available") {
    return;
  }
  try {
    const found = await invoke<Available | null>("app_update_check");
    useAppUpdateStore.setState({ checkedAt: Date.now() });
    // An install that started meanwhile keeps its state.
    const now = useAppUpdateStore.getState().update;
    if (now !== undefined && now.phase !== "available") {
      return;
    }
    useAppUpdateStore.setState({
      update:
        found === null
          ? undefined
          : { phase: "available", version: found.version },
    });
    if (manual && found === null) {
      notify("You're running the latest version");
    }
  } catch {
    if (manual) {
      notifyFailure("Couldn't check for updates");
    }
  }
};

/** Check for updates, in Settings: checks at once, whatever the preference says. */
export const checkAppUpdateNow = async () => {
  if (useAppUpdateStore.getState().checking) {
    return;
  }
  useAppUpdateStore.setState({ checking: true });
  await check(true);
  useAppUpdateStore.setState({ checking: false });
};

// How many times the user turned Check for updates on or off, so a read that started before a
// change does not undo it.
let changes = 0;

/** Turns the Check for updates preference on or off. A toast says when it could not be saved. */
export const setCheckUpdates = async (on: boolean) => {
  changes += 1;
  useAppUpdateStore.setState({ checkUpdates: on });
  try {
    await savePreferences({ check_updates: on });
  } catch {
    useAppUpdateStore.setState({ checkUpdates: !on });
    notifyFailure("Couldn't save the setting");
  }
};

/**
 * Reads the Check for updates preference into the store, and returns it. When the user changed it
 * while the read ran, the store keeps that change and this returns it.
 */
export const readCheckUpdates = async () => {
  const before = changes;
  const { check_updates: on } = await readPreferences();
  if (changes !== before) {
    return useAppUpdateStore.getState().checkUpdates ?? on;
  }
  useAppUpdateStore.setState({ checkUpdates: on });
  return on;
};

/** A background check, when the preference is on; it reads the preference each time. */
const checkInBackground = async () => {
  if (await readCheckUpdates()) {
    await check(false);
  }
};

/**
 * Starts the background checks: one now, then one a day. Call it once the startup checks pass,
 * so the startup stays offline. Returns what stops them.
 */
export const startAppUpdateChecks = (): (() => void) => {
  if (!inApp) {
    return () => {
      // A browser has no app to update.
    };
  }
  void checkInBackground();
  const timer = setInterval(() => {
    void checkInBackground();
  }, CHECK_EVERY_MS);
  return () => {
    clearInterval(timer);
  };
};

/** Downloads and installs the update found; the rim shows the download. */
export const installAppUpdate = async () => {
  const { update } = useAppUpdateStore.getState();
  if (update?.phase !== "available") {
    return;
  }
  const { version } = update;
  useAppUpdateStore.setState({
    rim: { fraction: 0, state: "running" },
    update: { phase: "installing", version },
  });
  let stop: (() => void) | undefined;
  try {
    stop = await listen<Progress>("app-update-progress", ({ payload }) => {
      if (payload.total === null || payload.total === 0) {
        return;
      }
      const fraction = payload.downloaded / payload.total;
      useAppUpdateStore.setState({
        rim: { fraction, state: "running" },
        update: {
          percent: Math.floor(fraction * 100),
          phase: "installing",
          version,
        },
      });
    });
    const newer = await invoke<string | null>("app_update_install", {
      version,
    });
    if (newer !== null) {
      // A check found a newer update meanwhile: nothing was installed, and the title bar offers it.
      useAppUpdateStore.setState({
        rim: { fraction: 0, state: "stopped" },
        update: { phase: "available", version: newer },
      });
      notifyFailure(`Update failed. agentx ${newer} is available`, {
        label: "Update",
        onClick: () => {
          void installAppUpdate();
        },
        track: "toast.app-update",
      });
      return;
    }
    useAppUpdateStore.setState({
      rim: { fraction: 1, state: "done" },
      update: { phase: "installed", version },
    });
  } catch {
    useAppUpdateStore.setState({
      rim: { fraction: 0, state: "stopped" },
      update: { phase: "available", version },
    });
    notifyFailure(`Couldn't install agentx ${version}`);
  } finally {
    stop?.();
  }
};

/** Restarts the app into the update it installed. */
export const restartApp = async () => {
  try {
    await invoke("app_restart");
  } catch {
    notifyFailure("Couldn't restart agentx. Quit and reopen it manually");
  }
};
