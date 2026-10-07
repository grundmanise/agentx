import { Icon } from "#/components/icon.tsx";
import { KeyHint } from "#/components/key-hint.tsx";

const isMac =
  typeof navigator !== "undefined" && navigator.platform.includes("Mac");

/** The command field in the title bar: looks like a search input and opens the palette. */
export const CommandField = ({ onOpen }: { onOpen: () => void }) => (
  <button
    type="button"
    onClick={onOpen}
    className="rounded-shell-command border-input-border bg-shell-content-bg type-command-field text-shell-topbar-text hover:border-input-border-focus hover:text-text-secondary h-command w-command flex items-center gap-2.5 border pr-1.5 pl-3.5"
  >
    <Icon name="search" size={14} strokeWidth={2} />
    <span className="flex-1 text-left">Search skills or run a command</span>
    <KeyHint>{isMac ? "⌘K" : "Ctrl K"}</KeyHint>
  </button>
);
