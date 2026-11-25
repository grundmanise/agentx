// This machine: the machine's label, which `config set label <value>` changes on Enter or when the
// field loses focus with a changed value, the machine id, and where the library and the store are
// (`storage.tsx`). The label and id come from the inventory's `machine`, and the field stays pending
// until the inventory after the change arrives. A refusal shows under the row as one
// sentence-cased paragraph (`ProblemText`), the label sent keeping its case.
import { useId, useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import { ProblemText } from "#/components/problem-text.tsx";
import { Input } from "#/components/ui/input.tsx";
import { SettingsCard, SettingsRow } from "#/screens/settings/section.tsx";
import { StorageRows } from "#/screens/settings/storage.tsx";
import type { Outcome } from "#/store/runner.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
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
  const pending = useSettling(LABEL_LABEL);
  const id = useId();

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
    const outcome = await runUntilSettled(["config", "set", "label", value], {
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
    <SettingsRow
      name="Label"
      line="Shown on skills you publish from here."
      htmlFor={id}
      control={
        <span className="w-settings-control flex">
          <Input
            id={id}
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
        </span>
      }
    >
      {problem !== undefined && (
        <ProblemText
          problem={problem}
          keep={sent === undefined ? [] : [sent]}
        />
      )}
    </SettingsRow>
  );
};

export const ThisMachineCard = () => {
  const label = useAppStore((s) => s.inventory?.machine.label);
  const id = useAppStore((s) => s.inventory?.machine.id);
  return (
    <SettingsCard title="This machine">
      {label !== undefined && <LabelField label={label} />}
      {id !== undefined && (
        <SettingsRow
          name="Machine id"
          control={
            <span className="type-technical text-text-hint select-text">
              {id}
            </span>
          }
        />
      )}
      <StorageRows />
    </SettingsCard>
  );
};
