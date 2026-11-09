// The machine the Settings tests run against: its `machine` event with the account repo and its
// size, and the machine settings `config list` reports.
import type { MachineEvent, Settings } from "#/cli/events.gen.ts";

import { libraryPath, machineLabel } from "../harness/cli.ts";
import type { InlineEvent } from "../harness/cli.ts";

export const HOME = "/home/me/.agentx";
export const ACCOUNT_REPO = "/home/me/.agentx/account.git";
export const MACHINE_ID = "3f2a9c1e5b7d40a8b6c2e9f1d0a4b8c7";

/** `machine`, with `label` in place of the machine's label. */
export const machine = (
  label = machineLabel
): Omit<MachineEvent, "schema_version"> => ({
  account_repo: ACCOUNT_REPO,
  // 1.5 MiB.
  account_repo_bytes: 1_572_864,
  derivation: "platform",
  home: HOME,
  id: MACHINE_ID,
  label,
  library: libraryPath,
  settings_file: `${HOME}/settings.json`,
  type: "machine",
});

/** `config list`, with `ignore_system_files` as given. */
export const settings = (ignoreSystemFiles: boolean): InlineEvent => {
  const value: Settings = {
    accept_operations: false,
    copy_mode: {},
    disabled_configurations: [],
    ignore_system_files: ignoreSystemFiles,
    label: machineLabel,
    schema_version: 1,
    sources: [],
  };
  return { settings: value, type: "settings" };
};
