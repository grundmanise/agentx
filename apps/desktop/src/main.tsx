import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { HashRouter } from "react-router";

import { App } from "#/app/app.tsx";
import { ErrorBoundary } from "#/components/error-boundary.tsx";
import { TooltipProvider } from "#/components/ui/tooltip.tsx";
import { followTelemetry, reportError } from "#/lib/telemetry.ts";
import { showKept, useStartup } from "#/startup/startup.ts";
import { useAppStore } from "#/store/store.ts";
import { liveSettings } from "#/store/telemetry.ts";

import "#/styles/index.css";

const root = document.querySelector("#root");
if (!root) {
  throw new Error("index.html has no #root element");
}

// Usage data follows the `telemetry` setting as this launch's inventory carries it: on, and with
// the anonymous id, which the CLI leaves out while DO_NOT_TRACK is set. The CLI's version, which
// the startup checks find before the first inventory, says whether this is a release build, and
// its channel.
const follow = () => {
  const settings = liveSettings(useAppStore.getState());
  const { version } = useStartup.getState().findings;
  followTelemetry(
    settings?.telemetry === "on",
    settings?.anonymous_id,
    version !== undefined && "cliVersion" in version
      ? version.cliVersion
      : undefined
  );
};
useAppStore.subscribe(follow);
useStartup.subscribe(follow);

showKept();
createRoot(root).render(
  <StrictMode>
    <ErrorBoundary
      onError={(error) => {
        void reportError(error);
      }}
    >
      {/* A navigation renders at once, not as a transition: the skill a command selects shows in
          the frame of the inventory that lists it. */}
      <HashRouter useTransitions={false}>
        <TooltipProvider>
          <App />
        </TooltipProvider>
      </HashRouter>
    </ErrorBoundary>
  </StrictMode>
);
