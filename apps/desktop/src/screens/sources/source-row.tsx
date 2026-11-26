import { StopPill } from "#/components/stop-pill.tsx";
import { Toggle } from "#/components/ui/toggle.tsx";
import { cn } from "#/lib/utils.ts";
import { rowPills, staleTitle } from "#/screens/sources/projection.ts";
import type { SourcePill } from "#/screens/sources/projection.ts";

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
 * The sources row of the Sources screen: one pill per shared source and Hub, the active one
 * lit, each with its skill count once it is known and the attention dot when it is stale, and a
 * Stop pill for each source being added, in the place its own pill will take. While a search
 * shows, each counts its matches instead.
 */
export const SourceRow = ({
  pills,
  adding,
  active,
  matches,
  now,
  onOpen,
  onStop,
}: {
  pills: SourcePill[];
  /** The links being added. */
  adding: readonly string[];
  /** The key of the active pill. */
  active: string | undefined;
  /** Each pill's count of matches, by its key, while a search shows. */
  matches?: ReadonlyMap<string, number>;
  /** Date.now(), for the stale dots. */
  now: number;
  onOpen: (key: string) => void;
  /** Stops the add of a link. */
  onStop: (link: string) => void;
}) => (
  <fieldset className="m-0 flex min-w-0 flex-wrap items-center gap-2 border-0 p-0">
    <legend className="sr-only">Sources</legend>
    {rowPills(pills, adding).map((entry) => {
      if (entry.kind === "adding") {
        return (
          <StopPill
            key={`adding ${entry.link}`}
            label={entry.label}
            stopLabel={`Stop adding ${entry.label}`}
            track="sources.adding-stop"
            onStop={() => {
              onStop(entry.link);
            }}
          />
        );
      }
      const { pill } = entry;
      const stale = staleTitle(pill.lastFetched, now);
      const pressed = pill.key === active;
      const shown = matches === undefined ? pill.skills : matches.get(pill.key);
      return (
        <Toggle
          key={pill.key}
          kind="source-pill"
          track="sources.pill-open"
          pressed={pressed}
          onPressedChange={() => {
            onOpen(pill.key);
          }}
          aria-label={pill.label}
        >
          {stale !== undefined && <StaleDot title={stale} />}
          {pill.label}
          {shown !== undefined && (
            <span
              className={cn(
                "type-technical-sm",
                pressed
                  ? "text-source-pill-active-count"
                  : "text-source-pill-count"
              )}
            >
              {shown}
            </span>
          )}
        </Toggle>
      );
    })}
  </fieldset>
);
