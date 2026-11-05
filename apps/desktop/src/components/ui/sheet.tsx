// shadcn/ui Sheet (`pnpm dlx shadcn@latest add sheet`), on our tokens: DESIGN.md §6 "Sheet".
// The design has one sheet, at the right edge, so there is no `side`. Its parts keep shadcn's
// names and `data-slot` attributes and take no `className`; the close is our ghost Button, in the
// header.
import { Dialog as SheetPrimitive } from "radix-ui";
import type { ReactNode } from "react";

import { CloseIcon } from "#/components/icons/close.tsx";
import { Button } from "#/components/ui/button.tsx";
import type { OwnProps } from "#/lib/utils.ts";

const Sheet = ({ ...props }: OwnProps<typeof SheetPrimitive.Root>) => (
  <SheetPrimitive.Root data-slot="sheet" {...props} />
);

const SheetTrigger = ({
  ...props
}: OwnProps<typeof SheetPrimitive.Trigger>) => (
  <SheetPrimitive.Trigger data-slot="sheet-trigger" {...props} />
);

const SheetClose = ({ ...props }: OwnProps<typeof SheetPrimitive.Close>) => (
  <SheetPrimitive.Close data-slot="sheet-close" {...props} />
);

const SheetPortal = ({ ...props }: OwnProps<typeof SheetPrimitive.Portal>) => (
  <SheetPrimitive.Portal data-slot="sheet-portal" {...props} />
);

/** `shell.scrim` behind the sheet. */
const SheetOverlay = ({
  ...props
}: OwnProps<typeof SheetPrimitive.Overlay>) => (
  <SheetPrimitive.Overlay
    data-slot="sheet-overlay"
    className="bg-shell-scrim fixed inset-0 z-40"
    {...props}
  />
);

/**
 * `sheet.width`, inset `sheet.inset` from the top, right and bottom, `sheet.bg` with
 * `sheet.border`, `sheet.radius`, `overlay.shadow` and `sheet.padding`. A column: the header,
 * then the content, which scrolls down only.
 */
const SheetContent = ({
  children,
  ...props
}: OwnProps<typeof SheetPrimitive.Content>) => (
  <SheetPortal>
    <SheetOverlay />
    <SheetPrimitive.Content
      data-slot="sheet-content"
      className="rounded-sheet border-sheet-border bg-sheet-bg shadow-overlay w-sheet top-sheet right-sheet bottom-sheet py-sheet-y px-sheet fixed z-40 flex flex-col gap-6 overflow-hidden border outline-none"
      {...props}
    >
      {children}
    </SheetPrimitive.Content>
  </SheetPortal>
);

/** The title and the description, with a ghost Close at the top right. */
const SheetHeader = ({ children }: { children: ReactNode }) => (
  <div data-slot="sheet-header" className="flex items-start gap-3">
    <div className="flex min-w-0 flex-1 flex-col gap-1">{children}</div>
    <SheetClose asChild>
      <Button variant="ghost" size="icon" aria-label="Close">
        <CloseIcon />
      </Button>
    </SheetClose>
  </div>
);

const SheetFooter = ({ ...props }: OwnProps<"div">) => (
  <div
    data-slot="sheet-footer"
    className="mt-auto flex justify-end gap-2"
    {...props}
  />
);

const SheetTitle = ({ ...props }: OwnProps<typeof SheetPrimitive.Title>) => (
  <SheetPrimitive.Title
    data-slot="sheet-title"
    className="type-dialog-title text-dialog-title m-0"
    {...props}
  />
);

/** Under the title, in mono: the name of what the sheet is about. */
const SheetDescription = ({
  ...props
}: OwnProps<typeof SheetPrimitive.Description>) => (
  <SheetPrimitive.Description
    data-slot="sheet-description"
    className="type-technical text-text-caption m-0 truncate"
    {...props}
  />
);

export {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetOverlay,
  SheetPortal,
  SheetTitle,
  SheetTrigger,
};
