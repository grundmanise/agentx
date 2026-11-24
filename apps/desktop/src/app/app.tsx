import { useWindowEvent } from "@mantine/hooks";
import { Activity, useEffect, useState } from "react";
import type { ReactElement } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";

import { ActivityLogSheet } from "#/app/activity-log.tsx";
import { startAppUpdateChecks } from "#/app/app-update.ts";
import { CommandPalette } from "#/app/command-palette.tsx";
import { KeptScreens, screenOf } from "#/app/kept-screens.tsx";
import { LaunchProgress } from "#/app/launch-progress.tsx";
import { SOURCES_PATH, titles } from "#/app/screens.ts";
import { Sidebar } from "#/app/sidebar.tsx";
import { StaleBar } from "#/app/stale-bar.tsx";
import { StartupLayer } from "#/app/startup-layer.tsx";
import { TelemetryDialog } from "#/app/telemetry-dialog.tsx";
import { TopBar } from "#/app/top-bar.tsx";
import { Toaster } from "#/components/ui/sonner.tsx";
import { setNavigate } from "#/lib/navigate.ts";
import { cn } from "#/lib/utils.ts";
import { AgentsScreen } from "#/screens/agents/agents-screen.tsx";
import { PlaceholderScreen } from "#/screens/placeholder.tsx";
import { PluginsScreen } from "#/screens/plugins/plugins-screen.tsx";
import { ServersScreen } from "#/screens/servers/servers-screen.tsx";
import { SettingsScreen } from "#/screens/settings/settings-screen.tsx";
import { SkillsScreen } from "#/screens/skills/skills-screen.tsx";
import { SourcesScreen } from "#/screens/sources/sources-screen.tsx";
import { launch, useStartup } from "#/startup/startup.ts";
import type { Step, StartupState } from "#/startup/startup.ts";
import { startLoaders } from "#/store/loaders.ts";

/** The screens built so far, by path; the others show their placeholder. */
const built: Record<string, ReactElement> = {
  "/agents": <AgentsScreen />,
  "/plugins": <PluginsScreen />,
  "/servers": <ServersScreen />,
  "/settings": <SettingsScreen />,
  "/skills": <SkillsScreen />,
  [SOURCES_PATH]: <SourcesScreen />,
};

// `isolate` keeps a screen's own layers, such as the list's pinned headers, under the frosted pane.
const elementOf = (path: string) =>
  built[path] ?? <PlaceholderScreen title={titles[path]} />;

/**
 * The screens. They mount once the startup checks pass, faded in, or with the first render when
 * the store holds the kept inventory: under the frosted pane, out of reach, until they pass.
 */
const Screen = ({ cached, ready }: Pick<StartupState, "cached" | "ready">) =>
  ready || cached ? (
    <div
      inert={!ready}
      className={cn(
        "isolate flex min-h-0 flex-1 flex-col transition-opacity duration-200",
        // The kept inventory is there with the first render: it does not fade in.
        !cached && "animate-fade-in",
        // Under the frosted pane, and out of reach, while it may not be this machine as it is now.
        !ready && "pointer-events-none select-none"
      )}
    >
      <KeptScreens elementOf={elementOf} />
    </div>
  ) : null;

/**
 * A frosted pane over the kept inventory while the startup checks run, with the launch spinner on
 * top. It takes every click and key, so nothing starts before the checks pass. When they pass it
 * fades out.
 */
const LaunchVeil = ({ ready, step }: { ready: boolean; step?: Step }) => {
  const [last, setLast] = useState(step);
  const [gone, setGone] = useState(false);
  if (step && step !== last) {
    setLast(step);
  }
  if (gone) {
    return null;
  }
  return (
    <div
      data-launch-veil
      className={cn(
        "bg-shell-loading-veil backdrop-blur-panel-loading absolute inset-0 z-10 flex",
        ready && "animate-code-loading-out pointer-events-none"
      )}
      onAnimationEnd={(event) => {
        if (ready && event.target === event.currentTarget) {
          setGone(true);
        }
      }}
    >
      {last && <LaunchProgress step={last} />}
    </div>
  );
};

export const App = () => {
  const [paletteOpen, setPaletteOpen] = useState(false);
  const startup = useStartup();
  // The layer closes the palette: it does not come back when the layer goes.
  if (startup.open && paletteOpen) {
    setPaletteOpen(false);
  }

  useEffect(() => {
    launch();
  }, []);

  // The store loads its slices once the checks pass: nothing runs a command before they do.
  useEffect(() => {
    if (startup.ready) {
      startLoaders();
    }
  }, [startup.ready]);

  // The app checks for its own update once the checks pass, so the startup stays offline.
  useEffect(
    () => (startup.ready ? startAppUpdateChecks() : undefined),
    [startup.ready]
  );

  // A route that is no screen's goes to Skills.
  const { pathname } = useLocation();
  const known = screenOf(pathname) !== undefined;

  // The command runner navigates from outside React, as on exit code 4.
  const navigate = useNavigate();
  useEffect(() => {
    setNavigate(navigate);
  }, [navigate]);

  // ⌘K / Ctrl+K toggles the palette from anywhere but the startup layer, once the checks pass.
  useWindowEvent("keydown", (e) => {
    const shortcut = (e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k";
    if (shortcut && startup.ready && !startup.open) {
      e.preventDefault();
      setPaletteOpen((open) => !open);
    }
  });

  return (
    <div className="bg-shell-bg text-text-primary flex h-full flex-col">
      <TopBar
        commandField={!startup.open}
        locked={!startup.ready}
        onOpenPalette={() => {
          setPaletteOpen(true);
        }}
      />
      {!known && <Navigate to="/skills" replace />}
      {startup.open && <StartupLayer />}
      {/* Hidden, not unmounted, while the startup layer shows: the screens stay as they were. */}
      <Activity mode={startup.open ? "hidden" : "visible"}>
        <div className="flex min-h-0 flex-1">
          {/* Until the checks pass, nothing in the window but the frosted pane can be reached. */}
          <div inert={!startup.ready} className="contents">
            <Sidebar />
          </div>
          <main
            aria-busy={!startup.ready}
            className="rounded-shell-panel border-shell-divider bg-shell-content-bg mr-panel mb-panel relative flex min-w-0 flex-1 flex-col overflow-hidden border"
          >
            {!(startup.ready || startup.cached) && startup.step && (
              <LaunchProgress step={startup.step} />
            )}
            {startup.cached && (
              <LaunchVeil ready={startup.ready} step={startup.step} />
            )}
            {startup.ready && <StaleBar />}
            {/* No command runs until the checks pass: one a screen starts waits for them. */}
            <Screen cached={startup.cached} ready={startup.ready} />
          </main>
        </div>
      </Activity>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
      <ActivityLogSheet />
      {startup.ready && <TelemetryDialog />}
      <Toaster />
    </div>
  );
};
