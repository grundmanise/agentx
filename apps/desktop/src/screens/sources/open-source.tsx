// The open source on the Sources screen: its card, what Refresh runs and the Remove dialog.
// Refresh fetches a shared source, `source fetch <id>`, whose listing the screen reads again once
// `source list` reports the new fetch; for Personal it reads your skills on the account remote
// again, `skill list --remote`, which fetches it.
import { useState } from "react";

import type { LibraryEntry } from "#/cli/events.gen.ts";
import { YOUR_SKILLS } from "#/screens/sources/projection.ts";
import type { SourcePill } from "#/screens/sources/projection.ts";
import {
  RemoveSourceDialog,
  removeSourceLabel,
} from "#/screens/sources/remove-source-dialog.tsx";
import { SourceDetail } from "#/screens/sources/source-detail.tsx";
import { loadInstallable } from "#/store/loaders.ts";
import { runCommand, usePending } from "#/store/runner.ts";

/** The pending label of Refresh on a shared source. */
export const REFRESH_LABEL = "refresh source";

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
}: {
  pill: SourcePill;
  library: LibraryEntry[];
  /** Date.now(), for the Fetched fact. */
  now: number;
}) => {
  const [removing, setRemoving] = useState(false);
  const refreshing = usePending(REFRESH_LABEL);
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
      />
      <RemoveSourceDialog
        source={pill.source}
        library={library}
        open={removing}
        onOpenChange={setRemoving}
      />
    </>
  );
};
