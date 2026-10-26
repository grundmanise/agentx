import { useEffect, useState } from "react";
import { Navigate, Outlet, Route, Routes, useNavigate } from "react-router";

import { CommandPalette } from "#/app/command-palette.tsx";
import { LaunchProgress } from "#/app/launch-progress.tsx";
import { screens, titles } from "#/app/screens.ts";
import { Sidebar } from "#/app/sidebar.tsx";
import { StaleBar } from "#/app/stale-bar.tsx";
import { StartupLayer } from "#/app/startup-layer.tsx";
import { TopBar } from "#/app/top-bar.tsx";
import { Toaster } from "#/components/toast.tsx";
import { setNavigate } from "#/lib/navigate.ts";
import { PlaceholderScreen } from "#/screens/placeholder.tsx";
import { launch, useStartup } from "#/startup/startup.ts";
import { startLoaders } from "#/store/loaders.ts";

/** The screen of the current route, faded in; nothing until the startup checks pass. */
const Screen = ({ ready }: { ready: boolean }) =>
  ready ? (
    <div className="animate-fade-in flex min-h-0 flex-1 flex-col">
      <Outlet />
    </div>
  ) : null;

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

  // ⌘K / Ctrl+K toggles the palette from anywhere but the startup layer.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const shortcut = (e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k";
      if (shortcut && !startup.open) {
        e.preventDefault();
        setPaletteOpen((open) => !open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, [startup.open]);

  return (
    <div className="bg-shell-bg text-text-primary flex h-full flex-col">
      <TopBar
        commandField={!startup.open}
        onOpenPalette={() => {
          setPaletteOpen(true);
        }}
      />
      {startup.open ? (
        <StartupLayer />
      ) : (
        <div className="flex min-h-0 flex-1">
          <Sidebar />
          <main
            aria-busy={!startup.ready}
            className="rounded-shell-panel border-shell-divider bg-shell-content-bg mr-panel mb-panel relative flex min-w-0 flex-1 flex-col overflow-hidden border"
          >
            {!startup.ready && startup.slow && startup.step && (
              <LaunchProgress step={startup.step} />
            )}
            {startup.ready && <StaleBar />}
            <Routes>
              {/* No screen mounts until the checks pass: none runs a command before they do. */}
              <Route element={<Screen ready={startup.ready} />}>
                {screens.map((s) => (
                  <Route
                    key={s.path}
                    path={`${s.path}/*`}
                    element={<PlaceholderScreen title={titles[s.path]} />}
                  />
                ))}
              </Route>
              <Route path="*" element={<Navigate to="/inbox" replace />} />
            </Routes>
          </main>
        </div>
      )}
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
      <Toaster />
    </div>
  );
};
