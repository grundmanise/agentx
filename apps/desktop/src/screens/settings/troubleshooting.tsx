// Troubleshooting: Command line, the CLI's version and git's as the startup checks found them
// (`version` and doctor's `git` row), with Run checks, which runs the startup checks again in a
// dialog (src/app/checks-dialog.tsx); and Activity log, which opens the log of the last commands
// the app ran (src/app/activity-log.tsx).
import { Button } from "#/components/ui/button.tsx";
import { SettingsCard, SettingsRow } from "#/screens/settings/section.tsx";
import { runChecks, useStartup } from "#/startup/startup.ts";
import { openActivityLog } from "#/store/activity-log.ts";

const CommandLineRow = () => {
  const version = useStartup((s) =>
    s.findings.version !== undefined && "cliVersion" in s.findings.version
      ? s.findings.version.cliVersion
      : undefined
  );
  const git = useStartup(
    (s) => s.findings.doctor?.rows.find((r) => r.check === "git")?.detail
  );
  let line: string | undefined;
  if (version !== undefined) {
    line =
      git === undefined ? `agentx ${version}` : `agentx ${version} · ${git}`;
  }
  return (
    <SettingsRow
      name="Command line"
      line={
        line === undefined ? undefined : (
          <span className="type-technical">{line}</span>
        )
      }
      control={
        <Button
          variant="secondary"
          size="sm"
          track="settings.cli-run-checks"
          onClick={runChecks}
        >
          Run checks
        </Button>
      }
    />
  );
};

export const TroubleshootingCard = () => (
  <SettingsCard title="Troubleshooting">
    <CommandLineRow />
    <SettingsRow
      name="Activity log"
      line="The last commands the app ran, with the failures in red."
      control={
        <Button
          variant="secondary"
          size="sm"
          track="settings.activity-log-show"
          onClick={() => {
            openActivityLog();
          }}
        >
          Show activity log
        </Button>
      }
    />
  </SettingsCard>
);
