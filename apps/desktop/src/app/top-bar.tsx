import { PublishStatus } from "#/app/publish-status.tsx";
import { CommandField } from "#/components/command-field.tsx";

/**
 * The 50px title bar. On macOS the window's own traffic lights sit in the left column; the whole
 * bar drags the window. The right column holds the sync status. The startup layer hides the
 * command field and the sync status; until the startup checks pass, both are `locked`: shown, but
 * out of reach. The bar still drags the window.
 */
export const TopBar = ({
  commandField,
  locked,
  onOpenPalette,
}: {
  commandField: boolean;
  locked: boolean;
  onOpenPalette: () => void;
}) => (
  <header
    data-tauri-drag-region
    className="h-titlebar flex flex-none items-center px-4"
  >
    <div data-tauri-drag-region className="flex-1 self-stretch" />
    {commandField && (
      <div inert={locked} className="contents">
        <CommandField onOpen={onOpenPalette} />
      </div>
    )}
    <div
      data-tauri-drag-region
      className="flex flex-1 items-center justify-end self-stretch"
    >
      {commandField && (
        <div inert={locked} className="contents">
          <PublishStatus />
        </div>
      )}
    </div>
  </header>
);
