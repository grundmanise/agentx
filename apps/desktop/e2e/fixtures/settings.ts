// The machine the Settings and telemetry tests run against: this machine and its settings as the
// inventory lists them, and its `machine` event with the store and its size, which Storage reads.
import type { Machine, MachineEvent } from "#/cli/events.gen.ts";

import { firstInventory, libraryPath, machineLabel } from "../harness/cli.ts";
import type { InlineEvent, MockCli } from "../harness/cli.ts";

export const HOME = "/home/me/.agentx";
export const STORE = "/home/me/.agentx/store.git";
export const MACHINE_ID = "3f2a9c1e5b7d40a8b6c2e9f1d0a4b8c7";

/** This machine as the inventory lists it, with `label` in place of its label. */
const thisMachine = (label = machineLabel): Machine => ({
  derivation: "platform",
  home: HOME,
  id: MACHINE_ID,
  label,
  library: libraryPath,
  settings_file: `${HOME}/settings.json`,
});

/** `machine`: this machine with the store and its size. */
export const machine = (): Omit<MachineEvent, "schema_version"> => ({
  ...thisMachine(),
  store: STORE,
  // 1.5 MiB.
  store_bytes: 1_572_864,
  type: "machine",
});

/**
 * An inventory of this machine, with its label, `ignore_system_files` and `telemetry` as given,
 * the anonymous id `anonymousId` names, if any, and `scanCounter` as its scan counter.
 */
export const inventoryWith = ({
  anonymousId,
  ignore = true,
  label = machineLabel,
  scanCounter = 1,
  telemetry,
}: {
  anonymousId?: string;
  ignore?: boolean;
  label?: string;
  scanCounter?: number;
  telemetry?: "on" | "off";
} = {}) =>
  ({
    ...firstInventory(),
    machine: thisMachine(label),
    scan_counter: scanCounter,
    settings: {
      ignore_system_files: ignore,
      ...(telemetry === undefined ? {} : { telemetry }),
      ...(anonymousId === undefined ? {} : { anonymous_id: anonymousId }),
    },
  }) satisfies InlineEvent;

/** Sends the inventory that follows a command, as serve does. */
export const scanned = async (
  cli: MockCli,
  inventory: ReturnType<typeof inventoryWith>
) => {
  await cli.serveEmit([inventory]);
};
