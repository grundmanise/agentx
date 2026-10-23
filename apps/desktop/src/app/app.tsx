import { useEffect, useState } from "react";
import { Navigate, Route, Routes } from "react-router";

import { CommandPalette } from "#/app/command-palette.tsx";
import { screens, titles } from "#/app/screens.ts";
import { Sidebar } from "#/app/sidebar.tsx";
import { StartupLayer } from "#/app/startup-layer.tsx";
import { TopBar } from "#/app/top-bar.tsx";
import { Toaster } from "#/components/toast.tsx";
import { PlaceholderScreen } from "#/screens/placeholder.tsx";
import { launch, useStartup } from "#/startup/startup.ts";

export const App = () => {
  const [paletteOpen, setPaletteOpen] = useState(false);
  const startup = useStartup();

  useEffect(() => {
    launch();
  }, []);

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
      {/* While the checks show, no screen mounts: none runs a command before they pass. */}
      {startup.open ? (
        <StartupLayer />
      ) : (
        <div className="flex min-h-0 flex-1">
          <Sidebar />
          <main className="rounded-shell-panel border-shell-divider bg-shell-content-bg mr-panel mb-panel relative flex min-w-0 flex-1 flex-col overflow-hidden border">
            <Routes>
              {screens.map((s) => (
                <Route
                  key={s.path}
                  path={`${s.path}/*`}
                  element={<PlaceholderScreen title={titles[s.path]} />}
                />
              ))}
              <Route path="*" element={<Navigate to="/inbox" replace />} />
            </Routes>
          </main>
        </div>
      )}
      <CommandPalette
        open={paletteOpen && !startup.open}
        onOpenChange={setPaletteOpen}
      />
      <Toaster />
    </div>
  );
};
