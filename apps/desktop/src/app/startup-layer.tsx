import { useWindowEvent } from "@mantine/hooks";
import { useNavigate } from "react-router";

import { AgentxMark } from "#/components/agentx-mark.tsx";
import { CheckRow } from "#/components/check-row.tsx";
import { CopyButton } from "#/components/copy-button.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { readChecks, report } from "#/startup/checks.ts";
import { closeChecks, retry, useStartup } from "#/startup/startup.ts";

/**
 * The startup layer: shows when a check that this machine can run agentx fails. It names the
 * first failure, shows the CLI's fix for it, and offers Retry. In manual mode (Settings › Run
 * checks) it shows every check and waits for Open agentx once everything passes.
 */
export const StartupLayer = () => {
  const { findings, mode, retrying, running } = useStartup();
  const navigate = useNavigate();
  const { checks, failure } = readChecks(findings, running);

  let title = "You're all set";
  let primary: { label: string; act: () => void } | undefined;
  if (running) {
    title = "Checking this machine";
  } else if (failure !== undefined) {
    title = failure;
    primary = { act: retry, label: "Retry" };
  } else if (mode === "manual") {
    primary = {
      act: () => {
        closeChecks();
        void navigate("/settings");
      },
      label: "Open agentx",
    };
  }
  const showReport = !running && (failure !== undefined || mode === "manual");

  // Enter triggers the primary action, unless focus is on a button, which handles Enter itself.
  const act = primary?.act;
  useWindowEvent("keydown", (e) => {
    const onButton =
      e.target instanceof Element && e.target.closest("button") !== null;
    if (e.key === "Enter" && act !== undefined && !onButton) {
      e.preventDefault();
      act();
    }
  });

  return (
    <section
      aria-label="Startup checks"
      className="bg-shell-bg flex min-h-0 flex-1 justify-center overflow-auto pb-11"
    >
      <div className="w-check-column gap-check-block my-auto flex flex-none flex-col py-6">
        <AgentxMark size={40} />
        <h1 className="type-startup-title text-text-primary m-0">{title}</h1>
        <ul
          aria-label="Checks"
          className="gap-check-row m-0 flex list-none flex-col p-0"
        >
          {checks.map((check) => (
            <CheckRow key={check.label} check={check} />
          ))}
        </ul>
        <div className="flex items-center gap-2">
          {running && (
            <Button variant="primary" disabled>
              <Spinner />
              {retrying ? "Retry" : "Checking"}
            </Button>
          )}
          {primary !== undefined && (
            <Button
              variant="primary"
              onClick={() => {
                primary.act();
              }}
            >
              {primary.label}
            </Button>
          )}
          {showReport && (
            <CopyButton label="Copy report" text={() => report(findings)} />
          )}
        </div>
      </div>
    </section>
  );
};
