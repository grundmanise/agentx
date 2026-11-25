// The `telemetry` setting: whether this machine sends anonymous usage data and crash reports, the
// CLI's and the app's alike (docs/help-center/telemetry.mdx). The inventory carries it, with the
// anonymous id while it is on. The telemetry dialog and the switch in Settings both answer it with
// `config set telemetry on|off`, and stay pending until the inventory that follows arrives; the
// app writes no file.
import type { InventorySettings } from "#/cli/events.gen.ts";
import { runUntilSettled } from "#/store/settle.ts";
import type { AppState } from "#/store/store.ts";

/** The help center page that lists what is sent. */
export const TELEMETRY_PAGE = "https://docs.agentx.wtf/telemetry";

/** The pending label of `config set telemetry`. */
export const TELEMETRY_LABEL = "set telemetry";

/**
 * The settings of the inventory a serve child sent this launch: undefined before it arrives. The
 * inventory kept from an earlier launch does not count, since the setting may have changed since.
 */
export const liveSettings = (s: AppState): InventorySettings | undefined =>
  s.applied === undefined ? undefined : s.inventory?.settings;

/**
 * Turns usage data on or off: `config set telemetry on|off`, then a scan, as after any command.
 * The control stays pending until the inventory that follows it arrives.
 */
export const setTelemetry = async (on: boolean) => {
  await runUntilSettled(["config", "set", "telemetry", on ? "on" : "off"], {
    label: TELEMETRY_LABEL,
    success: on ? "Sending anonymous usage data" : "Not sending usage data",
  });
};
