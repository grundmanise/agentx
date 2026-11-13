import { Toggle } from "#/components/ui/toggle.tsx";
import { cn } from "#/lib/utils.ts";
import { staleTitle } from "#/screens/sources/projection.ts";
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
 * The sources row of the Sources screen: one pill per shared source and Personal, the active one
 * lit, each with its skill count once it is known and the attention dot when it is stale.
 */
export const SourceRow = ({
  pills,
  active,
  now,
  onOpen,
}: {
  pills: SourcePill[];
  /** The key of the active pill. */
  active: string | undefined;
  /** Date.now(), for the stale dots. */
  now: number;
  onOpen: (key: string) => void;
}) => (
  <fieldset className="m-0 flex min-w-0 flex-wrap items-center gap-2 border-0 p-0">
    <legend className="sr-only">Sources</legend>
    {pills.map((pill) => {
      const stale = staleTitle(pill.lastFetched, now);
      const pressed = pill.key === active;
      return (
        <Toggle
          key={pill.key}
          kind="source-pill"
          pressed={pressed}
          onPressedChange={() => {
            onOpen(pill.key);
          }}
          aria-label={pill.label}
        >
          {stale !== undefined && <StaleDot title={stale} />}
          {pill.label}
          {pill.skills !== undefined && (
            <span
              className={cn(
                "type-technical-sm",
                pressed
                  ? "text-source-pill-active-count"
                  : "text-source-pill-count"
              )}
            >
              {pill.skills}
            </span>
          )}
        </Toggle>
      );
    })}
  </fieldset>
);
