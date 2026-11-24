// The app update in the title bar, left of the sync status (DESIGN.md §6 "App update (title
// bar)"), while the last check found one (`app-update.ts`): "Update to 0.4.0", whose click
// installs it, then "Installing 42%" with the download on its rim, then "Restart to update",
// whose click restarts the app into it. Nothing shows while there is no update.
import {
  installAppUpdate,
  restartApp,
  settleAppUpdateRim,
  useAppUpdate,
  useAppUpdateRim,
} from "#/app/app-update.ts";
import type { AppUpdate } from "#/app/app-update.ts";
import { ArrowUpIcon } from "#/components/icons/arrow-up.tsx";
import {
  StatusAnnouncement,
  StatusButton,
} from "#/components/status-button.tsx";

/** The label and tooltip of each phase. */
const describe = (update: AppUpdate): { label: string; tooltip: string[] } => {
  if (update.phase === "available") {
    return {
      label: `Update to ${update.version}`,
      tooltip: [`Install agentx ${update.version}`],
    };
  }
  if (update.phase === "installing") {
    return {
      label:
        update.percent === undefined
          ? "Installing…"
          : `Installing ${update.percent}%`,
      tooltip: [],
    };
  }
  return {
    label: "Restart to update",
    tooltip: [`Restart agentx to run ${update.version}`],
  };
};

export const AppUpdateStatus = () => {
  const update = useAppUpdate();
  const rim = useAppUpdateRim();
  if (update === undefined) {
    return null;
  }
  const { label, tooltip } = describe(update);
  return (
    <>
      <StatusButton
        tone="none"
        icon={
          <span className="text-accent-primary flex">
            <ArrowUpIcon size="2xs" />
          </span>
        }
        label={label}
        tooltip={tooltip}
        busy={update.phase === "installing"}
        rim={rim}
        onRimSettled={settleAppUpdateRim}
        onClick={
          update.phase === "installing"
            ? undefined
            : () => {
                void (update.phase === "available"
                  ? installAppUpdate()
                  : restartApp());
              }
        }
      />
      {/* The phase, never the percent, so a screen reader hears each change once. */}
      <StatusAnnouncement
        text={update.phase === "installing" ? "Installing…" : label}
      />
    </>
  );
};
