// Analytics and crash reporting: the Allow anonymous usage data switch, bound to the `telemetry` setting
// (src/store/telemetry.ts). It runs `config set telemetry on|off` and shows the setting as the
// inventory carries it, off while the setting has no value: pending while the command runs and
// until the inventory that follows it arrives.
import { useId } from "react";

import { Button } from "#/components/ui/button.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { openInBrowser } from "#/lib/open-url.ts";
import { SettingsCard, SettingsRow } from "#/screens/settings/section.tsx";
import { useSettling } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";
import {
  TELEMETRY_LABEL,
  TELEMETRY_PAGE,
  setTelemetry,
} from "#/store/telemetry.ts";

const SendUsageData = ({ checked }: { checked: boolean }) => {
  const id = useId();
  const pending = useSettling(TELEMETRY_LABEL);
  return (
    <SettingsRow
      name={<label htmlFor={id}>Allow anonymous usage data</label>}
      line={
        <>
          Help improve agentx with anonymous usage data.{" "}
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
        </>
      }
      control={
        <>
          {pending && (
            <span className="text-text-hint flex">
              <Spinner />
            </span>
          )}
          <Switch
            track="settings.usage-data"
            id={id}
            title={
              checked ? "On · usage data is sent" : "Off · nothing is sent"
            }
            checked={checked}
            disabled={pending}
            aria-busy={pending ? true : undefined}
            onCheckedChange={(on) => {
              void setTelemetry(on);
            }}
          />
        </>
      }
    />
  );
};

export const UsageDataCard = () => {
  const settings = useAppStore((s) => s.inventory?.settings);
  return (
    <SettingsCard title="Analytics and crash reporting">
      {settings !== undefined && (
        <SendUsageData checked={settings.telemetry === "on"} />
      )}
    </SettingsCard>
  );
};
