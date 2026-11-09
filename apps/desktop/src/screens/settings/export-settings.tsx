// The Export settings section of Settings: saves this machine's settings and the list of its
// skills to `<name>.json` in the library, a snapshot in the CLI's words
// (docs/help-center/cli/export.mdx). It runs `export <name>`, and `export <name> --force` once the
// user confirms an overwrite. The app writes no file: the CLI writes the snapshot.
import { useId, useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import { ProblemText } from "#/components/problem-text.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Input } from "#/components/ui/input.tsx";
import { Confirm } from "#/screens/settings/confirm.tsx";
import { SettingsSection } from "#/screens/settings/section.tsx";
import {
  DEFAULT_SNAPSHOT_NAME,
  isSnapshotName,
} from "#/screens/settings/transfer.ts";
import { runCommand, usePending } from "#/store/runner.ts";
import type { Problem } from "#/store/store.ts";

const EXPORT_LABEL = "export settings";

/** The file's name, and Export, which writes `<name>.json` to the library. */
const ExportForm = () => {
  const [name, setName] = useState(DEFAULT_SNAPSHOT_NAME);
  const [problem, setProblem] = useState<Problem | undefined>();
  const [overwrite, setOverwrite] = useState(false);
  const pending = usePending(EXPORT_LABEL);
  const hintId = useId();
  const valid = isSnapshotName(name);

  const exportAs = async (force: boolean) => {
    setProblem(undefined);
    const args = force ? ["export", name, "--force"] : ["export", name];
    const outcome = await runCommand(args, {
      label: EXPORT_LABEL,
      ownFailure: true,
      success: (events) => {
        const written = events.find((e) => e.type === "export");
        return `Exported settings to ${written?.path ?? `${name}.json`}`;
      },
    });
    if (outcome.exitCode === EXIT_CODES.ok) {
      return;
    }
    // A file of that name is already in the library: replacing it is the user's call.
    if (outcome.exitCode === EXIT_CODES.refused && !force) {
      setOverwrite(true);
      return;
    }
    const error = outcome.events.find((e) => e.type === "error");
    setProblem(
      error === undefined
        ? {
            message:
              outcome.startError ??
              `agentx export exited with code ${outcome.exitCode ?? "none"}`,
          }
        : { hint: error.hint, message: error.message }
    );
  };

  return (
    <form
      className="flex flex-col gap-2"
      aria-label="Export settings"
      onSubmit={(e) => {
        e.preventDefault();
        if (valid && !pending) {
          void exportAs(false);
        }
      }}
    >
      <div className="flex items-center gap-2">
        <Input
          kind="mono"
          aria-label="File name"
          aria-describedby={hintId}
          aria-invalid={valid && problem === undefined ? undefined : true}
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            setProblem(undefined);
          }}
        />
        <Button type="submit" variant="secondary" disabled={!valid || pending}>
          {pending && <Spinner />}
          Export
        </Button>
      </div>
      <p id={hintId} className="type-secondary text-text-muted m-0">
        {valid
          ? `Saved to your library as ${name}.json`
          : "Enter a file name without an extension or a slash"}
      </p>
      {problem !== undefined && <ProblemText problem={problem} />}
      <Confirm
        open={overwrite}
        onOpenChange={setOverwrite}
        title={`Replace ${name}.json?`}
        description="A file with this name is already in your library."
        action="Replace"
        onConfirm={() => {
          setOverwrite(false);
          void exportAs(true);
        }}
      />
    </form>
  );
};

export const ExportSettingsSection = () => (
  <SettingsSection
    title="Export settings"
    description="Saves your settings and the list of your skills to a file. Import it on another machine to transfer them."
  >
    <ExportForm />
  </SettingsSection>
);
