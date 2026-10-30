// Checking for a newer agentx: `agentx update --check` asks GitHub which version the CLI's
// channel offers and changes nothing. The toast says what it found in the CLI's own words. The
// app never runs `agentx update` itself: a release build runs the CLI inside its own bundle,
// which a new release of the app replaces.
import type { CliEvent } from "#/cli/events.gen.ts";
import { runCommand } from "#/store/runner.ts";

/** The control's label, which shows it as pending while the check runs. */
export const CHECK_UPDATES = "check-updates";

/** What the check found, from its `update` event. */
export const updateText = (events: CliEvent[]): string => {
  const found = events.find((e) => e.type === "update");
  if (found === undefined) {
    return "Checked for updates";
  }
  if (found.state === "available") {
    return `agentx ${found.version} is available, you have ${found.current_version}`;
  }
  const channel =
    found.channel === "nightly" ? "newest nightly" : "latest release";
  return `agentx ${found.version} is the ${channel}`;
};

/** Runs the check and toasts what it found. */
export const checkForUpdates = async () =>
  await runCommand(["update", "--check"], {
    label: CHECK_UPDATES,
    success: updateText,
  });
