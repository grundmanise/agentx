// Usage data: the Send usage data switch, bound to the `telemetry` setting (src/store/telemetry.ts).
// It runs `config set telemetry on|off` and shows the setting as the CLI reports it, off while the
// setting has no value: pending while the command runs and while the settings are read again.
import { useId } from "react";

import { Button } from "#/components/ui/button.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { openInBrowser } from "#/lib/open-url.ts";
import { SettingsSection } from "#/screens/settings/section.tsx";
import { loadLabel } from "#/store/loaders.ts";
import { usePending } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";
import {
  TELEMETRY_LABEL,
  TELEMETRY_PAGE,
  setTelemetry,
} from "#/store/telemetry.ts";

const SendUsageData = ({ checked }: { checked: boolean }) => {
  const id = useId();
  const running = usePending(TELEMETRY_LABEL);
  const reading = usePending(loadLabel(["config", "list"]));
  const pending = running || reading;
  return (
    <div className="flex items-start justify-between gap-6">
      <span className="flex min-w-0 flex-col gap-1">
        <label htmlFor={id} className="type-body text-text-primary">
          Send usage data
        </label>
        <span id={`${id}-line`} className="type-secondary text-text-muted">
          Commands, screens, clicks by name and crash reports, never a path, a
          name or anything you type.{" "}
          <Button
            track="settings.usage-data-help"
            variant="link"
            size="inline"
            onClick={() => {
              void openInBrowser(TELEMETRY_PAGE);
            }}
          >
            What is sent
          </Button>
        </span>
      </span>
      <span className="flex flex-none items-center gap-2">
        {pending && (
          <span className="text-text-hint flex">
            <Spinner />
          </span>
        )}
        <Switch
          track="settings.usage-data"
          id={id}
          aria-describedby={`${id}-line`}
          title={checked ? "On · usage data is sent" : "Off · nothing is sent"}
          checked={checked}
          disabled={pending}
          aria-busy={pending ? true : undefined}
          onCheckedChange={(on) => {
            void setTelemetry(on);
          }}
        />
      </span>
    </div>
  );
};

export const UsageDataSection = () => {
  const settings = useAppStore((s) => s.settings);
  return (
    <SettingsSection
      title="Usage data"
      description="Anonymous usage data and crash reports, from the CLI and the app."
    >
      {settings !== undefined && (
        <SendUsageData checked={settings.telemetry === "on"} />
      )}
    </SettingsSection>
  );
};
