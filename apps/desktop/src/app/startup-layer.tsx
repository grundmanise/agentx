import { useWindowEvent } from "@mantine/hooks";

import { AgentxMark } from "#/components/agentx-mark.tsx";
import { CheckList } from "#/components/check-row.tsx";
import { CopyButton } from "#/components/copy-button.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { checksTitle, readChecks, report } from "#/startup/checks.ts";
import { retry, useStartup } from "#/startup/startup.ts";

/**
 * The startup layer: shows when a check that this machine can run agentx fails. It names the
 * first failure, shows the CLI's fix for it, and offers Retry. The mark, the title and the buttons
 * are centred over the checks, which keep their left edge.
 */
export const StartupLayer = () => {
  const { findings, retrying, running } = useStartup();
  const read = readChecks(findings, running);
  const failed = !running && read.failure !== undefined;

  // Enter retries, unless focus is on a button, which handles Enter itself.
  useWindowEvent("keydown", (e) => {
    const onButton =
      e.target instanceof Element && e.target.closest("button") !== null;
    if (e.key === "Enter" && failed && !onButton) {
      e.preventDefault();
      retry();
    }
  });

  return (
    <section
      aria-label="Startup checks"
      className="bg-shell-bg flex min-h-0 flex-1 justify-center overflow-auto pb-11"
    >
      <div className="w-check-column gap-check-block my-auto flex flex-none flex-col items-center py-6">
        <AgentxMark size={40} />
        <h1 className="type-startup-title text-text-primary m-0 text-center">
          {checksTitle(read, running)}
        </h1>
        <div className="self-stretch">
          <CheckList checks={read.checks} />
        </div>
        <div className="flex items-center gap-2">
          {running && (
            <Button variant="primary" disabled track="startup.checking">
              <Spinner />
              {retrying ? "Retry" : "Checking"}
            </Button>
          )}
          {failed && (
            <Button variant="primary" track="startup.retry" onClick={retry}>
              Retry
            </Button>
          )}
          {failed && (
            <CopyButton
              label="Copy report"
              track="startup.copy-report"
              text={() => report(findings)}
            />
          )}
        </div>
      </div>
    </section>
  );
};
