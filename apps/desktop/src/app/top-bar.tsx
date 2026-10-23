import { CommandField } from "#/components/command-field.tsx";

/**
 * The 50px title bar. On macOS the window's own traffic lights sit in the left column; the whole
 * bar drags the window. The right column will hold the publish status once the app reads it. The
 * startup layer hides the command field.
 */
export const TopBar = ({
  commandField,
  onOpenPalette,
}: {
  commandField: boolean;
  onOpenPalette: () => void;
}) => (
  <header
    data-tauri-drag-region
    className="h-titlebar flex flex-none items-center px-4"
  >
    <div data-tauri-drag-region className="flex-1 self-stretch" />
    {commandField && <CommandField onOpen={onOpenPalette} />}
    <div data-tauri-drag-region className="flex-1 self-stretch" />
  </header>
);
