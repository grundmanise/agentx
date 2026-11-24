// Comparing edits: the Ignore system files switch, bound to the `ignore_system_files` setting. It
// runs `config set ignore_system_files true|false` and shows the setting as the CLI reports it:
// pending while the command runs and while the settings are read again after it.
import { SettingsSection } from "#/screens/settings/section.tsx";
import { SwitchRow } from "#/screens/settings/switch-row.tsx";
import { loadLabel } from "#/store/loaders.ts";
import { agentx, usePending } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";

/** The pending label of `config set ignore_system_files`. */
export const IGNORE_LABEL = "set ignore_system_files";

const IgnoreSystemFiles = ({ checked }: { checked: boolean }) => {
  const running = usePending(IGNORE_LABEL);
  const reading = usePending(loadLabel(["config", "list"]));
  return (
    <SwitchRow
      name="Ignore system files"
      line=".DS_Store, Thumbs.db and editor backups never count as edits. A skill's own .gitignore always applies."
      title={
        checked
          ? "On · system files never count as edits"
          : "Off · system files count as edits"
      }
      checked={checked}
      pending={running || reading}
      onChange={(on) => {
        void agentx(["config", "set", "ignore_system_files", String(on)], {
          label: IGNORE_LABEL,
          success: on
            ? "System files are ignored"
            : "System files count as edits now",
        });
      }}
    />
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
