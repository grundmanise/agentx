// The `telemetry` setting: whether this machine sends anonymous usage data and crash reports, the
// CLI's and the app's alike (docs/help-center/telemetry.mdx). The telemetry dialog and the switch
// in Settings both answer it with `config set telemetry on|off`; the app writes no file.
import { agentx } from "#/store/runner.ts";

/** The help center page that lists what is sent. */
export const TELEMETRY_PAGE = "https://docs.agentx.wtf/telemetry";

/** The pending label of `config set telemetry`. */
export const TELEMETRY_LABEL = "set telemetry";

/** Turns usage data on or off: `config set telemetry on|off`, then a scan, as after any command. */
export const setTelemetry = async (on: boolean) => {
  await agentx(["config", "set", "telemetry", on ? "on" : "off"], {
    label: TELEMETRY_LABEL,
    success: on ? "Sending anonymous usage data" : "Not sending usage data",
  });
};
