// The open source on the Sources screen: its card, what Refresh runs and the Remove dialog.
// Refresh fetches a shared source, `source fetch <id>`, whose listing the screen reads again once
// `source list` reports the new fetch; for Personal it reads your skills on the account remote
// again, `skill list --remote`, which fetches it.
import { useRef, useState } from "react";
import type { RefObject } from "react";

import type { LibraryEntry } from "#/cli/events.gen.ts";
import { YOUR_SKILLS } from "#/screens/sources/projection.ts";
import type { SourcePill } from "#/screens/sources/projection.ts";
import {
  RemoveSourceDialog,
  removeSourceLabel,
} from "#/screens/sources/remove-source-dialog.tsx";
import { SourceDetail } from "#/screens/sources/source-detail.tsx";
import { loadInstallable, loadLabel } from "#/store/loaders.ts";
import { runCommand, usePending } from "#/store/runner.ts";

/** The pending label of Refresh on a shared source. */
export const REFRESH_LABEL = "refresh source";

/** The pending label of what Refresh runs: on Personal, the read of your skills. */
const refreshLabel = (pill: SourcePill) =>
  pill.key === YOUR_SKILLS
    ? loadLabel(["skill", "list", "--remote"])
    : REFRESH_LABEL;

const refresh = (pill: SourcePill) => {
  if (pill.key === YOUR_SKILLS) {
    void loadInstallable();
  } else {
    void runCommand(["source", "fetch", pill.key], { label: REFRESH_LABEL });
  }
};

export const OpenSource = ({
  pill,
  library,
  now,
  fallbackFocus,
}: {
  pill: SourcePill;
  library: LibraryEntry[];
  /** Date.now(), for the Fetched fact. */
  now: number;
  /** Where focus goes when the Remove dialog closes while Remove cannot take it. */
  fallbackFocus: RefObject<HTMLElement | null>;
}) => {
  const [removing, setRemoving] = useState(false);
  const removeButton = useRef<HTMLButtonElement>(null);
  const refreshing = usePending(refreshLabel(pill));
  const pendingRemove = usePending(removeSourceLabel(pill.source.id));
  const onRefresh = () => {
    refresh(pill);
  };
  // The account remote is set, changed and detached in Settings.
  if (pill.key === YOUR_SKILLS) {
    return (
      <SourceDetail
        source={pill.source}
        skills={pill.skills}
        now={now}
        refreshing={refreshing}
        onRefresh={onRefresh}
      />
    );
  }
  return (
    <>
      <SourceDetail
        source={pill.source}
        skills={pill.skills}
        now={now}
        refreshing={refreshing}
        onRefresh={onRefresh}
        removing={pendingRemove}
        onRemove={() => {
          setRemoving(true);
        }}
        removeRef={removeButton}
      />
      <RemoveSourceDialog
        source={pill.source}
        library={library}
        open={removing}
        onOpenChange={setRemoving}
        onCloseAutoFocus={(event, confirmed) => {
          // Back to Remove after Cancel; after a removal its card goes, so the fallback takes it.
          event.preventDefault();
          const button = removeButton.current;
          (confirmed || button === null
            ? fallbackFocus.current
            : button
          )?.focus();
        }}
      />
    </>
  );
};
