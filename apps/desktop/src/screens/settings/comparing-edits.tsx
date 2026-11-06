// Comparing edits: the Ignore system files switch, bound to the `ignore_system_files` setting. It
// runs `config set ignore_system_files true|false` and shows the setting as the CLI reports it:
// pending while the command runs and while the settings are read again after it.
import { useId } from "react";

import { Spinner } from "#/components/spinner.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { SettingsSection } from "#/screens/settings/section.tsx";
import { loadLabel } from "#/store/loaders.ts";
import { runCommand, usePending } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";

/** The pending label of `config set ignore_system_files`. */
export const IGNORE_LABEL = "set ignore_system_files";

const IgnoreSystemFiles = ({ checked }: { checked: boolean }) => {
  const id = useId();
  const running = usePending(IGNORE_LABEL);
  const reading = usePending(loadLabel(["config", "list"]));
  const pending = running || reading;
  return (
    <label
      htmlFor={id}
      className="flex cursor-pointer items-start justify-between gap-6"
    >
      <span className="flex min-w-0 flex-col gap-1">
        <span className="type-body text-text-primary">Ignore system files</span>
        <span id={`${id}-line`} className="type-secondary text-text-muted">
          .DS_Store, Thumbs.db and editor backups never count as edits. A
          skill&apos;s own .gitignore always applies.
        </span>
      </span>
      <span className="flex flex-none items-center gap-2">
        {pending && (
          <span className="text-text-hint flex">
            <Spinner />
          </span>
        )}
        <Switch
          id={id}
          aria-label="Ignore system files"
          aria-describedby={`${id}-line`}
          title={
            checked
              ? "On · system files never count as edits"
              : "Off · system files count as edits"
          }
          checked={checked}
          disabled={pending}
          aria-busy={pending ? true : undefined}
          onCheckedChange={(on) => {
            void runCommand(
              ["config", "set", "ignore_system_files", String(on)],
              {
                label: IGNORE_LABEL,
                success: on
                  ? "System files are ignored"
                  : "System files count as edits now",
              }
            );
          }}
        />
      </span>
    </label>
  );
};

export const ComparingEditsSection = () => {
  const checked = useAppStore((s) => s.settings?.ignore_system_files);
  return (
    <SettingsSection
      title="Comparing edits"
      description="How agentx decides a skill was edited."
    >
      {checked !== undefined && <IgnoreSystemFiles checked={checked} />}
    </SettingsSection>
  );
};
