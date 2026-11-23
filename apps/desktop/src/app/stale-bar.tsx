import { Spinner } from "#/components/spinner.tsx";
import {
  Alert,
  AlertAction,
  AlertDescription,
} from "#/components/ui/alert.tsx";
import { Button } from "#/components/ui/button.tsx";
import { retryRefresh } from "#/store/feed.ts";
import { useAppStore } from "#/store/store.ts";

/**
 * A thin bar at the top of the main panel while the inventory shown may be out of date: while a
 * restarted serve child has not sent its first inventory, or after a refresh failed. An Alert in
 * its bar layout, an `<output>`, so a screen reader says when it shows or its text changes.
 */
export const StaleBar = () => {
  const freshness = useAppStore((s) => s.freshness);
  if (freshness.kind === "fresh") {
    return null;
  }
  return (
    <Alert
      layout="bar"
      tone={freshness.kind === "stale" ? "attention" : "danger"}
    >
      {freshness.kind === "stale" ? (
        <>
          <Spinner />
          <AlertDescription layout="bar">
            Reconnecting to agentx…
          </AlertDescription>
        </>
      ) : (
        <>
          <AlertDescription layout="bar">
            {`Couldn't read this machine: ${freshness.error}`}
          </AlertDescription>
          <AlertAction>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                void retryRefresh();
              }}
            >
              Retry
            </Button>
          </AlertAction>
        </>
      )}
    </Alert>
  );
};
