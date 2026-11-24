// The sync status at the right of the title bar: how this machine stands with the hub,
// from the inventory's library, the Sources slice and the update candidates. With no hub
// set, it says so and a click goes to Settings, where one is set. Otherwise it counts what
// there is to publish (hub skills edited since they were published, and those never
// published) and what is incoming (hub skills changed on your other machines), or says it
// is synced; a click opens a popover that lists both, with Publish all, and says when the hub
// was last checked. It runs no check itself: serve checks on its own, and Check updates on
// the Skills screen checks by hand.
import { useState } from "react";

import type { UpdateAvailableEvent } from "#/cli/events.gen.ts";
import {
  StatusAnnouncement,
  StatusButton,
} from "#/components/status-button.tsx";
import type { StatusTone } from "#/components/status-button.tsx";
import {
  SyncFooter,
  SyncGroup,
  SyncNote,
  SyncRow,
  SyncRows,
} from "#/components/sync-list.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "#/components/ui/popover.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { WithReason } from "#/components/with-reason.tsx";
import { formatAgo } from "#/lib/dates.ts";
import { navigateTo } from "#/lib/navigate.ts";
import { useNow } from "#/lib/now.ts";
import { hubOf, readOnlyHubReason } from "#/screens/skills/banners.ts";
import { openSkill } from "#/screens/skills/open.ts";
import {
  fromAnotherMachine,
  shortSource,
} from "#/screens/skills/projection.ts";
import {
  PUBLISH_ALL,
  publishAll,
  toPublish,
  settlePublishRim,
  usePublishProgress,
  usePublishRim,
} from "#/screens/skills/publish.ts";
import type { ToPublish } from "#/screens/skills/publish.ts";
import { useSkillRows } from "#/screens/skills/rows.ts";
import { useSettling } from "#/store/settle.ts";
import { sourcesOf, useAppStore } from "#/store/store.ts";

/**
 * The status, as the store says it: no hub, or with one, the skills to publish and
 * those incoming, each in name order.
 */
export type SyncState =
  | { kind: "no-hub" }
  | {
      kind: "hub";
      url: string;
      /** Why Publish all does nothing, while the hub is read-only. */
      readOnly?: string;
      outgoing: ToPublish[];
      incoming: UpdateAvailableEvent[];
    };

/** The sync status from the store; undefined until the first inventory, which carries the sources. */
export const useSyncState = (): SyncState | undefined => {
  const rows = useSkillRows();
  const inventory = useAppStore((s) => s.inventory);
  const updates = useAppStore((s) => s.updates);
  const hub = useAppStore((s) => hubOf(sourcesOf(s)));
  const readOnly = useAppStore((s) => readOnlyHubReason(sourcesOf(s)));
  if (inventory === undefined) {
    return undefined;
  }
  if (hub === undefined) {
    return { kind: "no-hub" };
  }
  // Only what another machine published to this hub, for a hub skill still
  // in the library: a skill removed since, a plugin's or a client's skill of the same name, or an
  // update from a hub set before this one, is not incoming.
  const installed = new Set(
    rows.filter((row) => row.kind === "hub").map((row) => row.name)
  );
  const incoming = Object.values(updates).filter(
    (update) =>
      fromAnotherMachine(update) &&
      update.source === hub.url &&
      installed.has(update.name)
  );
  incoming.sort((a, b) => a.name.localeCompare(b.name));
  return {
    incoming,
    kind: "hub",
    outgoing: toPublish(rows),
    readOnly,
    url: hub.url,
  };
};

/** What the label says while Publish all runs, from its progress. */
const publishingLabel = (progress?: { current: number; total?: number }) =>
  progress?.total === undefined
    ? "Publishing…"
    : `Publishing ${progress.current} of ${progress.total}`;

/** "2 edited · 1 new", leaving out a part that counts none. */
const outgoingLine = (outgoing: ToPublish[]) => {
  const edited = outgoing.filter((s) => s.kind === "edited").length;
  const added = outgoing.length - edited;
  return [
    ...(edited > 0 ? [`${edited} edited`] : []),
    ...(added > 0 ? [`${added} new`] : []),
  ].join(" · ");
};

/** The pill's tone, label and tooltip lines with a hub set. */
const describe = (
  outgoing: ToPublish[],
  incoming: UpdateAvailableEvent[],
  checked: string | undefined
): { tone: StatusTone; label: string; tooltip: string[] } => {
  const n = outgoing.length;
  const m = incoming.length;
  if (n === 0 && m === 0) {
    return {
      label: "Synced",
      tone: "synced",
      tooltip: [
        `Synced with your hub · ${checked === undefined ? "not checked yet" : `checked ${checked}`}`,
      ],
    };
  }
  return {
    label: [
      ...(n > 0 ? [`${n} to publish`] : []),
      ...(m > 0 ? [`${m} incoming`] : []),
    ].join(" · "),
    tone: "pending",
    tooltip: [
      ...(n > 0 ? [outgoingLine(outgoing)] : []),
      ...(m > 0 ? ["Changed on your other machines"] : []),
    ],
  };
};

export const PublishStatus = () => {
  const state = useSyncState();
  const publishing = useSettling(PUBLISH_ALL);
  const progress = usePublishProgress();
  const rim = usePublishRim();
  const lastChecked = useAppStore((s) => s.lastChecked);
  const now = useNow();
  const [open, setOpen] = useState(false);
  if (state === undefined) {
    return null;
  }
  if (state.kind === "no-hub") {
    return (
      <>
        <StatusButton
          tone="none"
          label="Set up sync"
          tooltip={["Connect a hub to sync skills"]}
          track="sync.set-up"
          onClick={() => {
            navigateTo("/settings");
          }}
        />
        <StatusAnnouncement text="Set up sync" />
      </>
    );
  }
  const { incoming, outgoing, readOnly, url } = state;
  const checked =
    lastChecked === undefined ? undefined : formatAgo(lastChecked, now);
  const shown = describe(outgoing, incoming, checked);
  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <StatusButton
            tone={shown.tone}
            label={publishing ? publishingLabel(progress) : shown.label}
            // The tooltip would cover the popover, and says nothing while the publish runs.
            tooltip={publishing || open ? [] : shown.tooltip}
            busy={publishing}
            rim={rim}
            onRimSettled={settlePublishRim}
            track="sync.open"
          />
        </PopoverTrigger>
        <PopoverContent aria-label="Sync" placement="below-end" width="fit">
          {outgoing.length === 0 && incoming.length === 0 && (
            <SyncNote>Everything is synced</SyncNote>
          )}
          {outgoing.length > 0 && (
            <SyncGroup caption="To publish">
              <SyncRows>
                {outgoing.map((skill) => (
                  <SyncRow
                    key={skill.name}
                    name={skill.name}
                    meta={skill.kind}
                    track="sync.outgoing-open"
                    onOpen={() => {
                      openSkill(skill.name);
                    }}
                  />
                ))}
              </SyncRows>
              <div className="mt-2 flex justify-end px-2">
                <WithReason reason={readOnly}>
                  <Button
                    variant="primary"
                    size="sm"
                    aria-busy={publishing}
                    track="sync.publish-all"
                    aria-disabled={readOnly === undefined ? undefined : true}
                    onClick={
                      readOnly === undefined
                        ? () => {
                            setOpen(false);
                            void publishAll(
                              outgoing.map((skill) => skill.name)
                            );
                          }
                        : undefined
                    }
                  >
                    {publishing && <Spinner />}
                    Publish all
                  </Button>
                </WithReason>
              </div>
            </SyncGroup>
          )}
          {incoming.length > 0 && (
            <SyncGroup caption="Incoming">
              <SyncRows>
                {incoming.map((update) => (
                  <SyncRow
                    key={update.name}
                    name={update.name}
                    track="sync.incoming-open"
                    onOpen={() => {
                      openSkill(update.name);
                    }}
                  />
                ))}
              </SyncRows>
            </SyncGroup>
          )}
          <SyncFooter
            remote={shortSource(url)}
            checked={
              checked === undefined ? "Not checked yet" : `Checked ${checked}`
            }
          />
        </PopoverContent>
      </Popover>
      {/* The summary alone, never the publish's progress, so a screen reader hears each change once. */}
      <StatusAnnouncement text={shown.label} />
    </>
  );
};
