// Command line: the CLI's version and git's, as the startup checks found them (`version` and
// doctor's `git` row), and Run checks, which opens the startup layer in manual mode; its Open
// agentx comes back here (src/app/startup-layer.tsx).
import { Button } from "#/components/ui/button.tsx";
import { SettingsSection } from "#/screens/settings/section.tsx";
import { runChecks, useStartup } from "#/startup/startup.ts";

export const CommandLineSection = () => {
  const version = useStartup((s) =>
    s.findings.version !== undefined && "cliVersion" in s.findings.version
      ? s.findings.version.cliVersion
      : undefined
  );
  const git = useStartup(
    (s) => s.findings.doctor?.rows.find((r) => r.check === "git")?.detail
  );
  return (
    <SettingsSection title="Command line">
      {version !== undefined && (
        <p className="type-technical text-text-secondary m-0">
          {git === undefined
            ? `agentx ${version}`
            : `agentx ${version} · ${git}`}
        </p>
      )}
      <div className="flex">
        <Button variant="secondary" onClick={runChecks}>
          Run checks
        </Button>
      </div>
    </SettingsSection>
  );
};
