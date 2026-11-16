import { cva } from "class-variance-authority";
import { useEffect, useState } from "react";
import type { ReactElement } from "react";
import { Navigate, Outlet, Route, Routes, useNavigate } from "react-router";

import { ActivityLogSheet } from "#/app/activity-log.tsx";
import { CommandPalette } from "#/app/command-palette.tsx";
import { LaunchProgress } from "#/app/launch-progress.tsx";
import { allScreens, SOURCES_PATH, titles } from "#/app/screens.ts";
import { Sidebar } from "#/app/sidebar.tsx";
import { StaleBar } from "#/app/stale-bar.tsx";
import { StartupLayer } from "#/app/startup-layer.tsx";
import { TopBar } from "#/app/top-bar.tsx";
import { Toaster } from "#/components/toast.tsx";
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

/** The screens built so far, by route; the others show their placeholder. */
const built: Record<string, ReactElement> = {
  "/agents": <AgentsScreen />,
  "/plugins": <PluginsScreen />,
  "/servers": <ServersScreen />,
  "/settings": <SettingsScreen />,
  "/skills": <SkillsScreen />,
  [SOURCES_PATH]: <SourcesScreen />,
};

const screenVariants = cva(
  "flex min-h-0 flex-1 flex-col transition-opacity duration-200",
  {
    variants: {
      // The kept inventory is there with the first render: it does not fade in.
      enter: { fade: "animate-fade-in", none: "" },
      // Under the frosted pane, and out of reach, while it may not be this machine as it is now.
      loading: {
        false: "",
        true: "pointer-events-none select-none",
      },
    },
  }
);

/**
 * The screen of the current route. It mounts once the startup checks pass, faded in, or with the
 * first render when the store holds the kept inventory: under the frosted pane, out of reach, until
 * they pass.
 */
const Screen = ({ cached, ready }: Pick<StartupState, "cached" | "ready">) =>
  ready || cached ? (
    <div
      inert={!ready}
      className={screenVariants({
        enter: cached ? "none" : "fade",
        loading: !ready,
      })}
    >
      <Outlet />
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

  // The command runner navigates from outside React, as on exit code 4.
  const navigate = useNavigate();
  useEffect(() => {
    setNavigate(navigate);
  }, [navigate]);

  // ⌘K / Ctrl+K toggles the palette from anywhere but the startup layer, once the checks pass.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const shortcut = (e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k";
      if (shortcut && startup.ready && !startup.open) {
        e.preventDefault();
        setPaletteOpen((open) => !open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, [startup.open, startup.ready]);

  return (
    <div className="bg-shell-bg text-text-primary flex h-full flex-col">
      <TopBar
        commandField={!startup.open}
        locked={!startup.ready}
        onOpenPalette={() => {
          setPaletteOpen(true);
        }}
      />
      {startup.open ? (
        <StartupLayer />
      ) : (
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
            <Routes>
              {/* No command runs until the checks pass: one a screen starts waits for them. */}
              <Route
                element={
                  <Screen cached={startup.cached} ready={startup.ready} />
                }
              >
                {allScreens.map((s) => (
                  <Route
                    key={s.path}
                    path={`${s.path}/*`}
                    element={
                      built[s.path] ?? (
                        <PlaceholderScreen title={titles[s.path]} />
                      )
                    }
                  />
                ))}
              </Route>
              <Route path="*" element={<Navigate to="/skills" replace />} />
            </Routes>
          </main>
        </div>
      )}
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
      <ActivityLogSheet />
      <Toaster />
    </div>
  );
};
