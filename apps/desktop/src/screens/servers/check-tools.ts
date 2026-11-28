// Check tools: `agentx scan --handshake --json` as a one-shot, with a deadline and a Cancel in its
// toast. Its inventory is never applied to the one the screens show (docs/spec/inventory-ordering.md):
// the scan stores what each server answered and why a handshake failed, and the next inventory of
// serve brings the tool counts, the failures and the time of the check in together. What the app
// takes from the one-shot is the toast's counts.
import type { CliEvent, McpServer } from "#/cli/events.gen.ts";
import { notify } from "#/components/toast.tsx";
import { checkSummary } from "#/screens/servers/projection.ts";
import { cancelCommand, agentx, usePending } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";

/** The control's label, which the runner shows as pending while the check runs. */
export const CHECK_TOOLS = "Check tools";

/** How long a check may run before it is stopped. */
export const CHECK_DEADLINE_MS = 120_000;

/** The server nodes of the one inventory a scan emits; none when it emitted no inventory. */
const serversOf = (events: CliEvent[]): McpServer[] =>
  events.find((e) => e.type === "inventory")?.mcp_servers ?? [];

/** Whether a check runs. */
export const useChecking = () => usePending(CHECK_TOOLS);

/** Runs the check, unless one runs already. */
export const checkTools = async () => {
  if (Object.hasOwn(useAppStore.getState().pending, CHECK_TOOLS)) {
    return;
  }
  notify(
    "Checking tools",
    {
      label: "Cancel",
      onClick: () => {
        void cancelCommand(CHECK_TOOLS);
      },
      track: "check-tools-toast.cancel",
    },
    { kind: "progress", lasting: true }
  );
  await agentx(["scan", "--handshake"], {
    deadlineMs: CHECK_DEADLINE_MS,
    label: CHECK_TOOLS,
    success: (events) => checkSummary(serversOf(events)),
  });
};
