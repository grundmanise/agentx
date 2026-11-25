// Updates: the app's version and when it last checked, Check for updates and Release notes, then
// the Automatic update checks switch, an app preference (ADR 0013, ADR 0019). Check for updates
// checks at once; once a check finds an update, the button follows it as the title bar does
// (`src/app/app-update-status.tsx`): Update to, Installing, Restart to update. Release notes opens
// this version's release on GitHub; a build that is no release, version 0.0.0, has none. A
// browser has no app to update, so there the card is left out.
import { getVersion } from "@tauri-apps/api/app";
import { useEffect, useId, useState } from "react";

import {
  checkAppUpdateNow,
  installAppUpdate,
  readCheckUpdates,
  restartApp,
  setCheckUpdates,
  useAppUpdate,
  useAppUpdateCheckedAt,
  useAppUpdateChecking,
  useCheckUpdates,
} from "#/app/app-update.ts";
import { Button } from "#/components/ui/button.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { formatAgo } from "#/lib/dates.ts";
import { useNow } from "#/lib/now.ts";
import { openInBrowser } from "#/lib/open-url.ts";
import { inApp } from "#/lib/platform.ts";
import { SettingsCard, SettingsRow } from "#/screens/settings/section.tsx";

const RELEASES = "https://github.com/grundmanise/agentx/releases/tag";

/** A build that is no release has this version, and no release page. */
const NO_RELEASE = "0.0.0";

/** The button at the right of the version: Check for updates, or the update a check found. */
const UpdateButton = () => {
  const update = useAppUpdate();
  const checking = useAppUpdateChecking();
  if (update?.phase === "available") {
    return (
      <Button
        variant="primary"
        size="sm"
        track="settings.app-update-install"
        onClick={() => {
          void installAppUpdate();
        }}
      >
        {`Update to ${update.version}`}
      </Button>
    );
  }
  if (update?.phase === "installing") {
    return (
      <Button
        variant="secondary"
        size="sm"
        track="settings.app-update-installing"
        aria-busy
        disabled
      >
        <Spinner />
        {update.percent === undefined
          ? "Installing…"
          : `Installing ${update.percent}%`}
      </Button>
    );
  }
  if (update?.phase === "installed") {
    return (
      <Button
        variant="primary"
        size="sm"
        track="settings.app-update-restart"
        onClick={() => {
          void restartApp();
        }}
      >
        Restart to update
      </Button>
    );
  }
  return (
    <Button
      variant="secondary"
      size="sm"
      track="settings.check-updates-now"
      aria-busy={checking ? true : undefined}
      disabled={checking}
      onClick={() => {
        void checkAppUpdateNow();
      }}
    >
      {checking && <Spinner />}
      Check for updates
    </Button>
  );
};

/** The version, when the app last checked or the update it found, and the buttons. */
const VersionRow = () => {
  const [version, setVersion] = useState<string>();
  const update = useAppUpdate();
  const checkedAt = useAppUpdateCheckedAt();
  const now = useNow();
  useEffect(() => {
    let current = true;
    void (async () => {
      const read = await getVersion();
      if (current) {
        setVersion(read);
      }
    })();
    return () => {
      current = false;
    };
  }, []);
  if (version === undefined) {
    return null;
  }
  let line = "Not checked yet";
  if (update?.phase === "available") {
    line = `agentx ${update.version} is available`;
  } else if (checkedAt !== undefined) {
    line = `Checked ${formatAgo(new Date(checkedAt).toISOString(), now) ?? "just now"}`;
  }
  return (
    <SettingsRow
      name={`agentx ${version}`}
      line={line}
      control={
        <>
          <UpdateButton />
          {version !== NO_RELEASE && (
            <Button
              variant="ghost"
              size="sm"
              track="settings.release-notes"
              onClick={() => {
                void openInBrowser(`${RELEASES}/v${version}`);
              }}
            >
              Release notes
            </Button>
          )}
        </>
      }
    />
  );
};

const AutomaticChecks = ({ checked }: { checked: boolean }) => {
  const id = useId();
  return (
    <SettingsRow
      name="Automatic update checks"
      line="After the app starts and once a day."
      lineId={`${id}-line`}
      htmlFor={id}
      control={
        <Switch
          id={id}
          track="settings.check-updates-toggle"
          aria-label="Automatic update checks"
          aria-describedby={`${id}-line`}
          checked={checked}
          onCheckedChange={(on) => {
            void setCheckUpdates(on);
          }}
        />
      }
    />
  );
};

export const AppUpdatesCard = () => {
  const checked = useCheckUpdates();
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
    <SettingsCard title="Updates">
      <VersionRow />
      {checked !== undefined && <AutomaticChecks checked={checked} />}
    </SettingsCard>
  );
};
