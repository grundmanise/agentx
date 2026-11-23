// App updates: the Check for updates switch, an app preference (ADR 0013, ADR 0019), and Check
// now, which checks at once. An update a check finds shows in the title bar, which installs it
// (`src/app/app-update.ts`). A browser has no app to update, so there the section is left out.
import { useEffect, useId } from "react";

import {
  checkAppUpdateNow,
  readCheckUpdates,
  setCheckUpdates,
  useAppUpdateChecking,
  useCheckUpdates,
} from "#/app/app-update.ts";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { inApp } from "#/lib/platform.ts";
import { SettingsSection } from "#/screens/settings/section.tsx";

const CheckUpdates = ({ checked }: { checked: boolean }) => {
  const id = useId();
  return (
    <label
      htmlFor={id}
      className="flex cursor-pointer items-start justify-between gap-6"
    >
      <span className="flex min-w-0 flex-col gap-1">
        <span className="type-body text-text-primary">Check for updates</span>
        <span id={`${id}-line`} className="type-secondary text-text-muted">
          After the app starts and once a day.
        </span>
      </span>
      <Switch
        id={id}
        aria-label="Check for updates"
        aria-describedby={`${id}-line`}
        checked={checked}
        onCheckedChange={(on) => {
          void setCheckUpdates(on);
        }}
      />
    </label>
  );
};

export const AppUpdatesSection = () => {
  const checked = useCheckUpdates();
  const checking = useAppUpdateChecking();
  // The preference, before the first background check reads it.
  useEffect(() => {
    if (inApp && checked === undefined) {
      void readCheckUpdates();
    }
  }, [checked]);
  if (!inApp) {
    return null;
  }
  return (
    <SettingsSection title="App updates">
      {checked !== undefined && <CheckUpdates checked={checked} />}
      <div className="flex">
        <Button
          variant="secondary"
          aria-busy={checking ? true : undefined}
          onClick={() => {
            void checkAppUpdateNow();
          }}
        >
          {checking && <Spinner />}
          Check now
        </Button>
      </div>
    </SettingsSection>
  );
};
