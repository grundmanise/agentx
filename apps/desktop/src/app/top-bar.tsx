import { PublishStatus } from "#/app/publish-status.tsx";
import { CommandField } from "#/components/command-field.tsx";

/**
 * The 50px title bar. On macOS the window's own traffic lights sit in the left column; the whole
 * bar drags the window. The right column holds the publish status. The startup layer hides the
 * command field and the publish status.
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
    <div
      data-tauri-drag-region
      className="flex flex-1 items-center justify-end self-stretch"
    >
      {commandField && <PublishStatus />}
    </div>
  </header>
);
