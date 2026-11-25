import { useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { FloatingBar, FloatingBarDivider } from "#/components/floating-bar.tsx";
import { CloseIcon } from "#/components/icons/close.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { andList, plural } from "#/lib/plural.ts";
import { removedText, removeFailure } from "#/screens/agents/remove.ts";
import { Confirm } from "#/screens/settings/confirm.tsx";
import { runUntilSettled, useSettling } from "#/store/settle.ts";

/** The runner's name for a command the bar runs. */
const BULK_LABEL = "selected agents";

const namesOf = (chosen: Configuration[]) => andList(chosen.map((c) => c.name));

interface BarValues {
  count: number;
  /** Some selected agent takes a placement and is disabled. */
  canEnable: boolean;
  /** Some selected agent takes a placement and is enabled. */
  canDisable: boolean;
  busy: boolean;
  onEnable: () => void;
  onDisable: () => void;
  onRemove: () => void;
  onClear: () => void;
}

const Bar = ({
  count,
  canEnable,
  canDisable,
  busy,
  onEnable,
  onDisable,
  onRemove,
  onClear,
}: BarValues) => (
  <>
    <span className="type-secondary text-text-muted flex items-center gap-2 whitespace-nowrap">
      {busy && <Spinner />}
      {count} selected
    </span>
    <FloatingBarDivider />
    <span className="gap-button flex items-center">
      <Button
        size="sm"
        track="agents.bulk-enable"
        disabled={busy || !canEnable}
        onClick={onEnable}
      >
        Enable
      </Button>
      <Button
        size="sm"
        track="agents.bulk-disable"
        disabled={busy || !canDisable}
        onClick={onDisable}
      >
        Disable
      </Button>
      <Button
        size="sm"
        variant="danger"
        track="agents.bulk-remove"
        disabled={busy}
        onClick={onRemove}
      >
        Remove
      </Button>
      <Button
        size="icon-sm"
        variant="ghost"
        track="agents.bulk-clear"
        aria-label="Clear selection"
        onClick={onClear}
      >
        <CloseIcon size="sm" />
      </Button>
    </span>
  </>
);

/**
 * The floating bar of the Agents page while any agent is selected: Enable and Disable, which
 * change the selected agents that take a placement (`config enable|disable <id>...`), and Remove,
 * which deletes every selected agent's configuration directory after one confirm dialog
 * (`config remove <id>... --yes`). Each clears the selection.
 */
export const BulkBar = ({
  chosen,
  onClear,
}: {
  /** The selected configurations, in inventory order. */
  chosen: Configuration[];
  onClear: () => void;
}) => {
  const [confirming, setConfirming] = useState<Configuration[]>();
  const busy = useSettling(BULK_LABEL);
  const direct = chosen.filter((c) => !c.reads_library);
  const toEnable = direct.filter((c) => !c.enabled);
  const toDisable = direct.filter((c) => c.enabled);

  const enable = () => {
    void runUntilSettled(["config", "enable", ...toEnable.map((c) => c.id)], {
      label: BULK_LABEL,
      success: `${namesOf(toEnable)} enabled`,
    });
    onClear();
  };
  const disable = () => {
    void runUntilSettled(["config", "disable", ...toDisable.map((c) => c.id)], {
      label: BULK_LABEL,
      success: `${namesOf(toDisable)} disabled. New skills won't go there by default.`,
    });
    onClear();
  };
  const remove = (gone: Configuration[]) => {
    void runUntilSettled(
      ["config", "remove", ...gone.map((c) => c.id), "--yes"],
      {
        failure: removeFailure,
        label: BULK_LABEL,
        success: removedText,
      }
    );
    onClear();
  };

  const [only] = confirming ?? [];
  return (
    <>
      <FloatingBar
        shown={chosen.length > 0}
        label="Selected agents"
        values={{
          busy,
          canDisable: toDisable.length > 0,
          canEnable: toEnable.length > 0,
          count: chosen.length,
          onClear,
          onDisable: disable,
          onEnable: enable,
          onRemove: () => {
            setConfirming(chosen);
          },
        }}
      >
        {(values) => <Bar {...values} />}
      </FloatingBar>
      <Confirm
        open={confirming !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setConfirming(undefined);
          }
        }}
        title={
          confirming?.length === 1 && only !== undefined
            ? `Remove ${only.name}?`
            : `Remove ${plural(confirming?.length ?? 0, "agent")}?`
        }
        description={
          confirming?.length === 1
            ? "Its configuration directory is deleted from this machine, with everything in it."
            : "Their configuration directories are deleted from this machine, with everything in them."
        }
        detail={confirming?.map((c) => c.path)}
        action="Remove"
        track="remove-agents-dialog.confirm"
        cancelTrack="remove-agents-dialog.cancel"
        onConfirm={() => {
          if (confirming !== undefined) {
            remove(confirming);
          }
          setConfirming(undefined);
        }}
      />
    </>
  );
};
