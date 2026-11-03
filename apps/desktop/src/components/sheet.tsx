import * as Dialog from "@radix-ui/react-dialog";
import type { ReactNode } from "react";

import { CloseIcon } from "#/components/icons/close.tsx";
import { Button } from "#/components/ui/button.tsx";

/**
 * A sheet at the right edge (DESIGN.md §2.2 "Overlays"): `sheet.width`, inset `sheet.inset` from
 * the top, right and bottom, `sheet.bg` with `sheet.border` and `overlay.shadow`, over
 * `shell.scrim`. Escape or the cross closes it.
 */
export const Sheet = ({
  open,
  onOpenChange,
  title,
  subtitle,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  /** Under the title, in mono: the name of what the sheet is about. */
  subtitle?: string;
  children: ReactNode;
}) => (
  <Dialog.Root open={open} onOpenChange={onOpenChange}>
    <Dialog.Portal>
      <Dialog.Overlay className="bg-shell-scrim fixed inset-0 z-40" />
      <Dialog.Content
        aria-describedby={undefined}
        className="rounded-sheet border-sheet-border bg-sheet-bg shadow-overlay w-sheet top-sheet right-sheet bottom-sheet py-sheet-y px-sheet fixed z-40 flex flex-col gap-6 overflow-hidden border outline-none"
      >
        <div className="flex items-start gap-3">
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <Dialog.Title className="type-dialog-title text-dialog-title m-0">
              {title}
            </Dialog.Title>
            {subtitle !== undefined && (
              <span className="type-technical text-text-caption truncate">
                {subtitle}
              </span>
            )}
          </div>
          <Dialog.Close asChild>
            <Button variant="ghost" size="icon" aria-label="Close">
              <CloseIcon />
            </Button>
          </Dialog.Close>
        </div>
        <div className="min-h-0 flex-1 overflow-auto">{children}</div>
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>
);
