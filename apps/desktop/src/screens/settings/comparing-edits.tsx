// Ignore system files, in the Hub card of Settings: the switch bound to the `ignore_system_files` setting. It
// runs `config set ignore_system_files true|false` and shows the setting as the inventory carries
// it: pending while the command runs and until the inventory that follows it arrives.
import { useId } from "react";

import { Spinner } from "#/components/ui/spinner.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { SettingsRow } from "#/screens/settings/section.tsx";
import { agentx, usePending } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";

/** The pending label of `config set ignore_system_files`. */
export const IGNORE_LABEL = "set ignore_system_files";

const IgnoreSystemFiles = ({ checked }: { checked: boolean }) => {
  const id = useId();
  const pending = usePending(IGNORE_LABEL);
  return (
    <SettingsRow
      name="Ignore system files"
      line=".DS_Store, Thumbs.db and editor backups never count as edits. A skill's own .gitignore always applies."
      lineId={`${id}-line`}
      htmlFor={id}
      control={
        <>
          {pending && (
            <span className="text-text-hint flex">
              <Spinner />
            </span>
          )}
          <Switch
            id={id}
            track="settings.ignore-system-files"
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
              void agentx(
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
        </>
      }
    />
  );
};

export const IgnoreSystemFilesRow = () => {
  const checked = useAppStore((s) => s.inventory?.settings.ignore_system_files);
  return checked === undefined ? null : <IgnoreSystemFiles checked={checked} />;
};
