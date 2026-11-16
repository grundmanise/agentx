// The card of the open source on the Sources screen (DESIGN.md §6 "Source card"): its owner's
// tile, its name, a Read-only tag when access is limited, its full URL with its pin beneath,
// which opens the repository in the browser, Refresh and Remove, and a strip of facts from
// `source list` and its listing. The name, the URL and the facts' values are selectable.
// The card of Personal, the account remote, has no Remove: Settings manages the account remote.
import { useRef } from "react";
import type { Ref } from "react";

import type { SourceEvent } from "#/cli/events.gen.ts";
import { RefreshIcon } from "#/components/icons/refresh.tsx";
import { Pill } from "#/components/pill.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { openInBrowser } from "#/lib/open-url.ts";
import { cn } from "#/lib/utils.ts";
import { shortId, shortSource } from "#/screens/skills/projection.ts";
import {
  fetchedFact,
  ownerInitial,
  webUrl,
} from "#/screens/sources/projection.ts";

/** The source as `source add` takes it: its URL, with `#<pin>` when pinned. */
const fullSource = (source: SourceEvent) =>
  source.pin === undefined ? source.url : `${source.url}#${source.pin}`;

/** The value of a fact the CLI does not report. */
const NONE = "–";

const Fact = ({
  label,
  value,
  attention = false,
}: {
  label: string;
  value: string;
  /** The value needs a look, in `tone.attention.fg`: a source never fetched. */
  attention?: boolean;
}) => (
  <div className="border-source-card-divider flex min-w-0 flex-col gap-0.5 border-l px-5 py-2.5 first:border-l-0">
    <dt className="type-secondary text-source-card-fact-label">{label}</dt>
    <dd
      className={cn(
        "type-technical-sm m-0 truncate select-text",
        attention ? "text-tone-attention-fg" : "text-source-card-fact-value"
      )}
    >
      {value}
    </dd>
  </div>
);

/** How far the pointer may move between press and release for the release to count as a click. */
const CLICK_SLOP = 4;

/**
 * The full URL under the name, a link to its repository when its URL names one. It is
 * selectable: a drag selects its text, and the release that ends the drag opens nothing.
 */
const SourceUrl = ({ source }: { source: SourceEvent }) => {
  const text = fullSource(source);
  const page = webUrl(source.url);
  const pressedAt = useRef<{ x: number; y: number } | null>(null);
  return (
    <p className="type-technical-sm text-text-muted m-0 truncate select-text">
      {page === undefined ? (
        text
      ) : (
        <a
          href={page}
          draggable={false}
          className="text-text-muted hover:underline"
          onPointerDown={(e) => {
            pressedAt.current = { x: e.clientX, y: e.clientY };
          }}
          onClick={(e) => {
            e.preventDefault();
            const from = pressedAt.current;
            pressedAt.current = null;
            const dragged =
              from !== null &&
              Math.hypot(e.clientX - from.x, e.clientY - from.y) > CLICK_SLOP;
            if (!dragged) {
              void openInBrowser(page);
            }
          }}
        >
          {text}
        </a>
      )}
    </p>
  );
};

export const SourceDetail = ({
  source,
  skills,
  now,
  refreshing,
  onRefresh,
  removing,
  onRemove,
  removeRef,
}: {
  source: SourceEvent;
  /** How many skills it holds, once known. */
  skills: number | undefined;
  /** Date.now(), for the Fetched fact. */
  now: number;
  /** The source is being fetched again. */
  refreshing: boolean;
  onRefresh: () => void;
  /** The source is being removed. */
  removing?: boolean;
  /** Opens the Remove dialog; absent for the account remote. */
  onRemove?: () => void;
  /** The Remove button, which focus returns to when its dialog closes. */
  removeRef?: Ref<HTMLButtonElement>;
}) => (
  <section
    aria-label="Source details"
    className="rounded-source-card border-source-card-border bg-source-card-bg flex flex-col border"
  >
    <div className="flex items-start gap-3.5 px-5 pt-4.5">
      <span
        aria-hidden
        className="rounded-source-card-tile bg-source-card-tile-bg text-source-card-tile-text type-source-name flex size-9 flex-none items-center justify-center"
      >
        {ownerInitial(source.url)}
      </span>
      <div className="flex min-h-9 min-w-0 flex-1 flex-col justify-center gap-0.5">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <h2 className="type-source-name text-source-card-name m-0 min-w-0 truncate select-text">
            {shortSource(source.url)}
          </h2>
          {source.access === "read-only" && (
            <Pill tone="neutral">Read-only</Pill>
          )}
        </div>
        <SourceUrl source={source} />
      </div>
      <div className="gap-button flex flex-none items-center">
        <Button
          variant="secondary"
          size="sm"
          aria-busy={refreshing}
          disabled={refreshing}
          onClick={onRefresh}
        >
          {refreshing ? <Spinner /> : <RefreshIcon size="sm" />}
          Refresh
        </Button>
        {onRemove !== undefined && (
          <Button
            ref={removeRef}
            variant="danger"
            size="sm"
            aria-busy={removing}
            disabled={removing}
            onClick={onRemove}
          >
            {removing === true && <Spinner />}
            Remove
          </Button>
        )}
      </div>
    </div>
    <dl className="border-source-card-divider m-0 mt-4 grid grid-cols-4 border-t">
      <Fact
        label="Skills"
        value={skills === undefined ? NONE : String(skills)}
      />
      <Fact label="Branch" value={source.default_branch ?? NONE} />
      <Fact
        label="Commit"
        value={source.commit === undefined ? NONE : shortId(source.commit)}
      />
      <Fact
        label="Fetched"
        value={fetchedFact(source.last_fetched, now)}
        attention={source.last_fetched === undefined}
      />
    </dl>
  </section>
);
