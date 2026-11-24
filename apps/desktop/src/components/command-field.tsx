import { CommandKeyIcon } from "#/components/icons/command-key.tsx";
import { SearchIcon } from "#/components/icons/search.tsx";
import { Kbd, KbdGroup } from "#/components/ui/kbd.tsx";
import { isMac } from "#/lib/platform.ts";

/** The command field in the title bar: looks like a search input and opens the palette. */
export const CommandField = ({ onOpen }: { onOpen: () => void }) => (
  <button
    type="button"
    onClick={onOpen}
    className="rounded-shell-command border-input-border bg-shell-content-bg type-command-field text-shell-topbar-text hover:border-input-border-focus hover:text-text-secondary h-command w-command flex items-center gap-2.5 border pr-1.5 pl-3.5"
  >
    <SearchIcon size="xs" />
    <span className="flex-1 text-left">Search skills or run a command</span>
    <KbdGroup>
      {isMac ? (
        <Kbd>
          <CommandKeyIcon />
          <span className="sr-only">⌘</span>
        </Kbd>
      ) : (
        <Kbd>Ctrl</Kbd>
      )}
      <Kbd>K</Kbd>
    </KbdGroup>
  </button>
);
