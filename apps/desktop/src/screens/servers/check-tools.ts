// Check tools: `agentx scan --handshake --json` as a one-shot, with a deadline and a Cancel in its
// toast. Its inventory is never applied to the one the screens show (docs/spec/inventory-ordering.md):
// the runner asks serve to scan again, which brings the stored signatures in. What the app keeps of
// the one-shot is its outcome: the declarations whose handshake failed, shown in the Tools column
// until the next check or a restart, and the toast's counts.
import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { CliEvent, McpServer } from "#/cli/events.gen.ts";
import { notify } from "#/components/toast.tsx";
import { checkSummary, failuresIn } from "#/screens/servers/projection.ts";
import { cancelCommand, runCommand, usePending } from "#/store/runner.ts";
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
    },
    { lasting: true }
  );
  const outcome = await runCommand(["scan", "--handshake"], {
    deadlineMs: CHECK_DEADLINE_MS,
    label: CHECK_TOOLS,
    success: (events) => checkSummary(serversOf(events)),
  });
  if (outcome.exitCode === EXIT_CODES.ok) {
    useAppStore.setState({
      handshakeFailures: failuresIn(serversOf(outcome.events)),
    });
  }
};
