import { cva } from "class-variance-authority";

import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { retryRefresh } from "#/store/feed.ts";
import { useAppStore } from "#/store/store.ts";

const barVariants = cva(
  "h-stale-bar px-stale-bar type-notice text-text-secondary flex flex-none items-center gap-3 border-b",
  {
    variants: {
      kind: {
        failed: "border-tone-danger-border bg-tone-danger-bg",
        stale: "border-tone-attention-border bg-tone-attention-bg",
      },
    },
  }
);

/**
 * A thin bar at the top of the main panel while the inventory shown may be out of date: while a
 * restarted serve child has not sent its first inventory, or after a refresh failed.
 */
export const StaleBar = () => {
  const freshness = useAppStore((s) => s.freshness);
  if (freshness.kind === "fresh") {
    return null;
  }
  return (
    <output className={barVariants({ kind: freshness.kind })}>
      {freshness.kind === "stale" ? (
        <>
          <Spinner />
          <span>Reconnecting to agentx…</span>
        </>
      ) : (
        <>
          <span className="min-w-0 flex-1 truncate">
            {`Couldn't read this machine: ${freshness.error}`}
          </span>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              void retryRefresh();
            }}
          >
            Retry
          </Button>
        </>
      )}
    </output>
  );
};
