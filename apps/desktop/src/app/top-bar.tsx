import { CommandField } from "@/components/command-field";

/**
 * The 50px title bar. On macOS the window's own traffic lights sit in the left column; the whole
 * bar drags the window. The right column will hold the publish status once the app reads it.
 */
export function TopBar({ onOpenPalette }: { onOpenPalette: () => void }) {
  return (
    <header data-tauri-drag-region className="flex h-(--shell-titlebar-height) flex-none items-center px-4">
      <div data-tauri-drag-region className="flex-1 self-stretch" />
      <CommandField onOpen={onOpenPalette} />
      <div data-tauri-drag-region className="flex-1 self-stretch" />
    </header>
  );
}
