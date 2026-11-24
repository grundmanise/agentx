// The machine the Settings tests run against: its `machine` event with the store and its
// size, and the machine settings `config list` reports.
import type { MachineEvent, Settings } from "#/cli/events.gen.ts";

import { libraryPath, machineLabel } from "../harness/cli.ts";
import type { InlineEvent } from "../harness/cli.ts";

export const HOME = "/home/me/.agentx";
export const STORE = "/home/me/.agentx/store.git";
export const MACHINE_ID = "3f2a9c1e5b7d40a8b6c2e9f1d0a4b8c7";

/** `machine`, with `label` in place of the machine's label. */
export const machine = (
  label = machineLabel
): Omit<MachineEvent, "schema_version"> => ({
  derivation: "platform",
  home: HOME,
  id: MACHINE_ID,
  label,
  library: libraryPath,
  settings_file: `${HOME}/settings.json`,
  store: STORE,
  // 1.5 MiB.
  store_bytes: 1_572_864,
  type: "machine",
});

/** `config list`, with `ignore_system_files` and `report_inventory` as given. */
export const settings = (
  ignoreSystemFiles: boolean,
  reportInventory = false
): InlineEvent => {
  const value: Settings = {
    accept_operations: false,
    copy_configurations: [],
    copy_mode: {},
    disabled_configurations: [],
    ignore_system_files: ignoreSystemFiles,
    label: machineLabel,
    report_inventory: reportInventory,
    schema_version: 1,
    sources: [],
  };
  return { settings: value, type: "settings" };
};
