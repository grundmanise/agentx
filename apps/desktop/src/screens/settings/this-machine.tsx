// This machine: the machine's label, which `config set label <value>` changes on Enter or when the
// field loses focus with a changed value, and the machine id. Both come from the `machine` event,
// which the store reads again after the label changes (src/store/loaders.ts). A refusal shows
// under the field as one sentence-cased paragraph (`ProblemText`), the label sent keeping its case.
import { useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import { ProblemText } from "#/components/problem-text.tsx";
import { Input } from "#/components/ui/input.tsx";
import { SettingsSection } from "#/screens/settings/section.tsx";
import { runCommand, usePending } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";
import type { Problem } from "#/store/store.ts";

/** The pending label of `config set label`. */
export const LABEL_LABEL = "set label";

/** What the CLI said went wrong, as it said it. */
const problemOf = (outcome: Outcome): Problem => {
  const error = outcome.events.find((e) => e.type === "error");
  if (error !== undefined) {
    return error.hint === undefined
      ? { message: error.message }
      : { hint: error.hint, message: error.message };
  }
  return {
    message:
      outcome.startError ??
      `agentx config set exited with code ${outcome.exitCode ?? "none"}`,
  };
};

const LabelField = ({ label }: { label: string }) => {
  const [draft, setDraft] = useState(label);
  const [shown, setShown] = useState(label);
  const [problem, setProblem] = useState<Problem | undefined>();
  // The value last sent: losing focus does not send it again.
  const [sent, setSent] = useState<string | undefined>();
  const pending = usePending(LABEL_LABEL);

  // A new label from the CLI replaces what the field holds.
  if (shown !== label) {
    setShown(label);
    setDraft(label);
    setProblem(undefined);
    setSent(undefined);
  }

  const save = async (value: string) => {
    if (value === label || LABEL_LABEL in useAppStore.getState().pending) {
      return;
    }
    setSent(value);
    setProblem(undefined);
    const outcome = await runCommand(["config", "set", "label", value], {
      label: LABEL_LABEL,
      ownFailure: true,
      success: "Label saved",
    });
    // A busy lock and a stop are the runner's to report; anything else shows under the field.
    if (
      outcome.exitCode !== EXIT_CODES.ok &&
      outcome.exitCode !== EXIT_CODES.locked &&
      !outcome.cancelled
    ) {
      setProblem(problemOf(outcome));
    }
  };

  return (
    <div className="gap-input-label flex flex-col">
      <Input
        aria-label="Machine label"
        aria-busy={pending ? true : undefined}
        aria-invalid={problem === undefined ? undefined : true}
        readOnly={pending}
        value={draft}
        onChange={(e) => {
          setDraft(e.target.value);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter") {
            e.preventDefault();
            void save(draft);
          }
        }}
        onBlur={() => {
          if (draft !== sent) {
            void save(draft);
          }
        }}
      />
      {problem !== undefined && (
        <ProblemText
          problem={problem}
          keep={sent === undefined ? [] : [sent]}
        />
      )}
    </div>
  );
};

export const ThisMachineSection = () => {
  const label = useAppStore((s) => s.machine?.label);
  const id = useAppStore((s) => s.machine?.id);
  return (
    <SettingsSection
      title="This machine"
      description="Shown on skills you publish from here."
    >
      {label !== undefined && <LabelField label={label} />}
      {id !== undefined && (
        <p className="type-technical text-text-hint m-0" title="Machine id">
          {id}
        </p>
      )}
    </SettingsSection>
  );
};
