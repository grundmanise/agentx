// Updates: the app's version with its Release notes, when it last checked and Check for updates, then
// the Automatic update checks and Include nightly builds switches, app preferences (ADR 0013,
// ADR 0019). Check for updates
// checks at once; once a check finds an update, the button follows it as the title bar does
// (`src/app/app-update-status.tsx`): Update to, Installing, Restart to update. Release notes opens
// this version's release on GitHub, and What's new the release of the update a check found; a
// build that is no release, version 0.0.0, has no Release notes. The version and both switches
// come with the app's settings (`src/lib/app-settings.ts`), so the rows show together. A
// browser has no app to update, so there the card is left out.
import { useId } from "react";
import type { ReactNode } from "react";

import {
  checkAppUpdateNow,
  includesNightlies,
  installAppUpdate,
  restartApp,
  setCheckUpdates,
  setIncludeNightlies,
  useAppUpdate,
  useAppUpdateCheckedAt,
  useAppUpdateChecking,
} from "#/app/app-update.ts";
import { Button } from "#/components/ui/button.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { useAppSettings } from "#/lib/app-settings.ts";
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

/** A link to a version's release on GitHub. */
const ReleaseLink = ({
  version,
  track,
  label,
}: {
  version: string;
  track: string;
  label: string;
}) => (
  <Button
    variant="link"
    size="inline"
    track={track}
    onClick={() => {
      void openInBrowser(`${RELEASES}/v${version}`);
    }}
  >
    {label}
  </Button>
);

/** The version with its Release notes, when the app last checked or the update it found, and the button. */
const VersionRow = ({ version }: { version: string }) => {
  const update = useAppUpdate();
  const checkedAt = useAppUpdateCheckedAt();
  const now = useNow();
  let line: ReactNode = "Not checked yet";
  if (update?.phase === "available") {
    line = (
      <>
        agentx {update.version} is available.{" "}
        <ReleaseLink
          version={update.version}
          track="settings.whats-new"
          label="What's new"
        />
      </>
    );
  } else if (checkedAt !== undefined) {
    line = `Checked ${formatAgo(new Date(checkedAt).toISOString(), now) ?? "just now"}`;
  }
  return (
    <SettingsRow
      name={
        version === NO_RELEASE ? (
          `agentx ${version}`
        ) : (
          <span className="flex items-baseline gap-2">
            agentx {version}
            <ReleaseLink
              version={version}
              track="settings.release-notes"
              label="Release notes"
            />
          </span>
        )
      }
      line={line}
      control={<UpdateButton />}
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

const IncludeNightlies = ({ checked }: { checked: boolean }) => {
  const id = useId();
  return (
    <SettingsRow
      name="Include nightly builds"
      line="Builds of main, published daily. They may be less stable."
      lineId={`${id}-line`}
      htmlFor={id}
      control={
        <Switch
          id={id}
          track="settings.include-nightlies-toggle"
          aria-label="Include nightly builds"
          aria-describedby={`${id}-line`}
          checked={checked}
          onCheckedChange={(on) => {
            void setIncludeNightlies(on);
          }}
        />
      }
    />
  );
};

export const AppUpdatesCard = () => {
  const settings = useAppSettings((s) => s);
  if (!inApp) {
    return null;
  }
  return (
    <SettingsCard title="Updates">
      {settings !== undefined && (
        <>
          <VersionRow version={settings.version} />
          <AutomaticChecks checked={settings.preferences.check_updates} />
          <IncludeNightlies checked={includesNightlies(settings)} />
        </>
      )}
    </SettingsCard>
  );
};
