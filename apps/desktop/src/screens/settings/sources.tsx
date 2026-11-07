// The Sources section of Settings: one row per shared source of the Sources slice, with when it
// was fetched, how many skills its listing holds, its pin and what this machine may do there, and
// Remove, which runs `source remove <url>` and offers Undo, `source add <url>[#<pin>]`. + Add source
// opens the Add skills screen's Add source dialog. The rows are the slice that `source list` fills
// again after each command: a removed source goes when the CLI no longer lists it.
import { useEffect, useMemo, useRef, useState } from "react";

import type { SourceEvent } from "#/cli/events.gen.ts";
import { AddIcon } from "#/components/icons/add.tsx";
import { Pill } from "#/components/pill.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { plural } from "#/lib/plural.ts";
import { AddSourceDialog } from "#/screens/add/add-source-dialog.tsx";
import { useNow } from "#/screens/settings/clock.ts";
import { SettingsSection } from "#/screens/settings/section.tsx";
import {
  accessLine,
  addAgainArgument,
  fetchedLine,
  sharedSources,
} from "#/screens/settings/source-rows.ts";
import { shortSource } from "#/screens/skills/projection.ts";
import { loadLabel, loadListing, loadSources } from "#/store/loaders.ts";
import { runCommand, usePending } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";

/** The pending label of `source remove` of the source `id`. */
export const removeLabel = (id: string) => `remove source ${id}`;

/** The pending label of the Undo of a removal, `source add` of the source `id`. */
export const undoLabel = (id: string) => `add source ${id} again`;

/** Adds the source again as it was before Remove: its URL, with its pin. */
const addAgain = (source: SourceEvent) => {
  void runCommand(["source", "add", addAgainArgument(source)], {
    label: undoLabel(source.id),
    success: (events) => {
      const added = events.find((e) => e.type === "source");
      return `Added ${added?.url ?? source.url}: ${plural(added?.skills ?? 0, "skill")}`;
    },
  });
};

const remove = (source: SourceEvent) => {
  void runCommand(["source", "remove", source.url], {
    action: {
      label: "Undo",
      onClick: () => {
        addAgain(source);
      },
    },
    label: removeLabel(source.id),
    success: `Removed ${shortSource(source.url)}. Skills from it stay as they are.`,
  });
};

const SourceItem = ({
  source,
  skills,
  now,
}: {
  source: SourceEvent;
  /** How many skills its listing holds; undefined until read. */
  skills: number | undefined;
  now: number;
}) => {
  const removing = usePending(removeLabel(source.id));
  return (
    <li className="hover:bg-table-row-hover rounded-row py-row pr-row-right pl-row-left flex items-start gap-3">
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <div className="gap-row-inline flex min-w-0 items-center">
          <p className="type-technical text-text-primary m-0 min-w-0 truncate">
            {source.url}
          </p>
          {source.pin !== undefined && (
            <Pill tone="neutral" face="mono">{`#${source.pin}`}</Pill>
          )}
        </div>
        <p className="type-secondary text-text-muted m-0">
          {fetchedLine(source, skills, now)}
        </p>
        <p className="type-secondary text-text-hint m-0">
          {accessLine(source.access)}
        </p>
      </div>
      <Button
        variant="ghost"
        size="sm"
        aria-label={`Remove ${source.url}`}
        disabled={removing}
        onClick={() => {
          remove(source);
        }}
      >
        {removing && <Spinner />}
        Remove
      </Button>
    </li>
  );
};

/**
 * The Sources section. When it opens it reads the sources again, as the Add skills screen does,
 * unless a read is on its way, and the listing of each fetched source, for its skill count:
 * `source list` reports no count for a shared source.
 */
export const SourcesSection = () => {
  const all = useAppStore((s) => s.sources);
  const listings = useAppStore((s) => s.listings);
  const sources = useMemo(() => sharedSources(all), [all]);
  const now = useNow();
  const [dialogOpen, setDialogOpen] = useState(false);
  // The fetch each listing was read for: source id → last_fetched.
  const read = useRef(new Map<string, string>());

  // A read already on its way, such as the store's own at launch, answers for this one.
  useEffect(() => {
    if (!(loadLabel(["source", "list"]) in useAppStore.getState().pending)) {
      void loadSources();
    }
  }, []);

  useEffect(() => {
    for (const source of sources) {
      const fetched = source.last_fetched;
      if (fetched !== undefined && read.current.get(source.id) !== fetched) {
        read.current.set(source.id, fetched);
        void loadListing(source.id);
      }
    }
  }, [sources]);

  return (
    <SettingsSection
      title="Sources"
      description="Git repositories you add skills from. agentx checks them when it opens and when you check for updates."
    >
      {sources.length > 0 && (
        <ul aria-label="Sources" className="gap-rows m-0 flex flex-col p-0">
          {sources.map((source) => (
            <SourceItem
              key={source.id}
              source={source}
              skills={listings[source.id]?.length}
              now={now}
            />
          ))}
        </ul>
      )}
      <div className="flex">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => {
            setDialogOpen(true);
          }}
        >
          <AddIcon size="sm" />
          Add source
        </Button>
      </div>
      {/* The new source shows once `source list` lists it, which the store reads after the add. */}
      <AddSourceDialog open={dialogOpen} onOpenChange={setDialogOpen} />
    </SettingsSection>
  );
};
