import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Toggle } from "#/components/ui/toggle.tsx";
import { fetchedLabel, staleTitle } from "#/screens/add/projection.ts";
import type { SourcePill } from "#/screens/add/projection.ts";

/** The `tone.attention.fg` dot on a pill whose source was not fetched for two days, or never. */
const StaleDot = ({ title }: { title: string }) => (
  <span title={title} className="flex">
    <svg
      viewBox="0 0 6 6"
      aria-hidden
      focusable="false"
      className="block size-1.5 flex-none"
    >
      <circle cx="3" cy="3" r="3" className="fill-tone-attention-fg" />
    </svg>
  </span>
);

/**
 * The sources row of the Add skills screen: one pill per shared source and Your skills, the
 * active one lit; at the right, Select all and Clear for the cards shown, and the refresh button
 * labelled with the active source's last fetch.
 */
export const SourceRow = ({
  pills,
  active,
  now,
  onOpen,
  canSelectAll,
  onSelectAll,
  canClear,
  onClear,
  refreshing,
  onRefresh,
}: {
  pills: SourcePill[];
  /** The key of the active pill. */
  active: string | undefined;
  /** Date.now(), for the fetch labels. */
  now: number;
  onOpen: (key: string) => void;
  canSelectAll: boolean;
  onSelectAll: () => void;
  canClear: boolean;
  onClear: () => void;
  /** The active source is being fetched. */
  refreshing: boolean;
  onRefresh: () => void;
}) => {
  const current = pills.find((p) => p.key === active);
  return (
    <div className="flex items-center gap-3">
      <fieldset className="m-0 flex min-w-0 flex-1 flex-wrap items-center gap-2 border-0 p-0">
        <legend className="sr-only">Sources</legend>
        {pills.map((pill) => {
          const stale = staleTitle(pill.lastFetched, now);
          return (
            <Toggle
              key={pill.key}
              kind="source-pill"
              pressed={pill.key === active}
              onPressedChange={() => {
                onOpen(pill.key);
              }}
              aria-label={pill.label}
            >
              {pill.label}
              {stale !== undefined && <StaleDot title={stale} />}
            </Toggle>
          );
        })}
      </fieldset>
      {current !== undefined && (
        <div className="flex flex-none items-center gap-1">
          {canSelectAll && (
            <Button variant="ghost" size="sm" onClick={onSelectAll}>
              Select all
            </Button>
          )}
          {canClear && (
            <Button variant="ghost" size="sm" onClick={onClear}>
              Clear
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            title={`Fetch ${current.label} again`}
            disabled={refreshing}
            onClick={onRefresh}
          >
            {refreshing && <Spinner />}
            {fetchedLabel(current.lastFetched, now)}
          </Button>
        </div>
      )}
    </div>
  );
};
