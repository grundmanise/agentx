import { Icon } from "#/components/icon.tsx";
import { KeyHint } from "#/components/key-hint.tsx";

const isMac = typeof navigator !== "undefined" && /Mac/.test(navigator.platform);

/** The command field in the title bar: looks like a search input and opens the palette. */
export function CommandField({ onOpen }: { onOpen: () => void }) {
  return (
    <button
      type="button"
      onClick={onOpen}
      className="flex h-(--shell-command-height) w-(--shell-command-width) items-center gap-2.5 rounded-shell-command border border-input-border bg-shell-content-bg pr-1.5 pl-3.5 type-command-field text-shell-topbar-text hover:border-input-border-focus hover:text-text-secondary"
    >
      <Icon name="search" size={14} strokeWidth={2} />
      <span className="flex-1 text-left">Search skills or run a command</span>
      <KeyHint>{isMac ? "⌘K" : "Ctrl K"}</KeyHint>
    </button>
  );
}
