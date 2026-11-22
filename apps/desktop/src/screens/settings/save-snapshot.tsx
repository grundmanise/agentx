// The Save snapshot section of Settings: saves this machine's settings and the list of its skills
// to `<name>.json` in the library, a snapshot (docs/help-center/cli/snapshot.mdx). It runs
// `snapshot save <name>`, and `snapshot save <name> --force` once the user confirms an overwrite.
// The app writes no file: the CLI writes the snapshot.
import { useId, useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import { ProblemText } from "#/components/problem-text.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Input } from "#/components/ui/input.tsx";
import { revealInFinder } from "#/lib/reveal.ts";
import { Confirm } from "#/screens/settings/confirm.tsx";
import { SettingsSection } from "#/screens/settings/section.tsx";
import {
  DEFAULT_SNAPSHOT_NAME,
  isSnapshotName,
} from "#/screens/settings/transfer.ts";
import { agentx, usePending } from "#/store/runner.ts";
import type { Problem } from "#/store/store.ts";

const SAVE_LABEL = "save snapshot";

/** The file's name, and Save, which writes `<name>.json` to the library. */
const SaveForm = () => {
  const [name, setName] = useState(DEFAULT_SNAPSHOT_NAME);
  const [problem, setProblem] = useState<Problem | undefined>();
  const [overwrite, setOverwrite] = useState(false);
  const pending = usePending(SAVE_LABEL);
  const hintId = useId();
  const valid = isSnapshotName(name);

  const save = async (force: boolean) => {
    setProblem(undefined);
    const args = force
      ? ["snapshot", "save", name, "--force"]
      : ["snapshot", "save", name];
    const outcome = await agentx(args, {
      // The toast shows where the file went, and Show opens it in the file manager.
      action: (events) => {
        const written = events.find((e) => e.type === "export");
        const path = written?.type === "export" ? written.path : undefined;
        return path === undefined
          ? undefined
          : {
              label: "Show",
              onClick: () => {
                void revealInFinder(path);
              },
            };
      },
      label: SAVE_LABEL,
      ownFailure: true,
      success: (events) => {
        const written = events.find((e) => e.type === "export");
        return `Saved snapshot to ${written?.path ?? `${name}.json`}`;
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
              `agentx snapshot save exited with code ${outcome.exitCode ?? "none"}`,
          }
        : { hint: error.hint, message: error.message }
    );
  };

  return (
    <form
      className="flex flex-col gap-2"
      aria-label="Save snapshot"
      onSubmit={(e) => {
        e.preventDefault();
        if (valid && !pending) {
          void save(false);
        }
      }}
    >
      <div className="flex items-center gap-2">
        <Input
          kind="mono"
          aria-label="File name"
          aria-describedby={valid ? undefined : hintId}
          aria-invalid={valid && problem === undefined ? undefined : true}
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            setProblem(undefined);
          }}
        />
        <Button type="submit" variant="secondary" disabled={!valid || pending}>
          {pending && <Spinner />}
          Save
        </Button>
      </div>
      {!valid && (
        <p id={hintId} className="type-secondary text-text-muted m-0">
          Enter a file name without an extension or a slash
        </p>
      )}
      {problem !== undefined && <ProblemText problem={problem} keep={[name]} />}
      <Confirm
        open={overwrite}
        onOpenChange={setOverwrite}
        title={`Replace ${name}.json?`}
        description="A file with this name is already in your library."
        action="Replace"
        onConfirm={() => {
          setOverwrite(false);
          void save(true);
        }}
      />
    </form>
  );
};

export const SaveSnapshotSection = () => (
  <SettingsSection
    title="Save snapshot"
    description="Save your settings and library to back up or move to another device."
  >
    <SaveForm />
  </SettingsSection>
);
