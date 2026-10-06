import { Icon } from "@/components/icon";

const isMac = typeof navigator !== "undefined" && /Mac/.test(navigator.platform);

/**
 * The 50px title bar. On macOS the window's own traffic lights sit in the left column; the whole
 * bar drags the window. The right column will hold the publish status once the app reads it.
 */
export function TopBar({ onOpenPalette }: { onOpenPalette: () => void }) {
  return (
    <header
      data-tauri-drag-region
      className="grid h-(--shell-titlebar-height) flex-none grid-cols-[1fr_auto_1fr] items-center px-4"
    >
      <div data-tauri-drag-region />
      <button
        type="button"
        onClick={onOpenPalette}
        className="flex h-(--shell-command-height) w-(--shell-command-width) items-center gap-2.5 rounded-full border border-input-border bg-shell-content-bg pr-1.5 pl-3.5 text-[13px] text-shell-topbar-text hover:border-input-border-focus hover:text-text-secondary"
      >
        <Icon name="search" size={14} strokeWidth={2} />
        <span className="flex-1 text-left">Search skills or run a command</span>
        <kbd className="rounded-full bg-button-secondary-bg px-2 py-[3px] font-mono text-[11px] text-text-muted">
          {isMac ? "⌘K" : "Ctrl K"}
        </kbd>
      </button>
      <div data-tauri-drag-region />
    </header>
  );
}
