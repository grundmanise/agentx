import { useEffect, useState } from "react";
import { Navigate, Route, Routes } from "react-router";
import { Toaster } from "@/components/toast";
import { PlaceholderScreen } from "@/screens/placeholder";
import { CommandPalette } from "./command-palette";
import { screens, titles } from "./screens";
import { Sidebar } from "./sidebar";
import { TopBar } from "./top-bar";

export function App() {
  const [paletteOpen, setPaletteOpen] = useState(false);

  // ⌘K / Ctrl+K toggles the palette from anywhere.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((open) => !open);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  return (
    <div className="flex h-full flex-col bg-shell-bg text-text-primary">
      <TopBar onOpenPalette={() => setPaletteOpen(true)} />
      <div className="flex min-h-0 flex-1">
        <Sidebar />
        <main className="relative mr-(--shell-panel-inset) mb-(--shell-panel-inset) flex min-w-0 flex-1 flex-col overflow-hidden rounded-shell-panel border border-shell-divider bg-shell-content-bg">
          <Routes>
            {screens.map((s) => (
              <Route key={s.path} path={`${s.path}/*`} element={<PlaceholderScreen title={titles[s.path]} />} />
            ))}
            <Route path="*" element={<Navigate to="/inbox" replace />} />
          </Routes>
        </main>
      </div>
      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
      <Toaster />
    </div>
  );
}
