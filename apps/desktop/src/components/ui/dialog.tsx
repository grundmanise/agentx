// shadcn/ui Dialog (`pnpm dlx shadcn@latest add dialog`), on our tokens: DESIGN.md §2.2
// "Overlays", §4 "Overlays" and §5. Its parts keep shadcn's names and `data-slot` attributes and
// take no `className`: the content's placement is a variant, and the close is our ghost Button.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Dialog as DialogPrimitive } from "radix-ui";
import type { ReactNode } from "react";

import { CloseIcon } from "#/components/icons/close.tsx";
import { Button } from "#/components/ui/button.tsx";
import type { OwnProps } from "#/lib/utils.ts";

const Dialog = ({ ...props }: OwnProps<typeof DialogPrimitive.Root>) => (
  <DialogPrimitive.Root data-slot="dialog" {...props} />
);

const DialogTrigger = ({
  ...props
}: OwnProps<typeof DialogPrimitive.Trigger>) => (
  <DialogPrimitive.Trigger data-slot="dialog-trigger" {...props} />
);

const DialogPortal = ({
  ...props
}: OwnProps<typeof DialogPrimitive.Portal>) => (
  <DialogPrimitive.Portal data-slot="dialog-portal" {...props} />
);

const DialogClose = ({ ...props }: OwnProps<typeof DialogPrimitive.Close>) => (
  <DialogPrimitive.Close data-slot="dialog-close" {...props} />
);

/** `shell.scrim-strong` behind a dialog and the palette. */
const DialogOverlay = ({
  ...props
}: OwnProps<typeof DialogPrimitive.Overlay>) => (
  <DialogPrimitive.Overlay
    data-slot="dialog-overlay"
    className="bg-shell-scrim-strong fixed inset-0 z-50"
    {...props}
  />
);

const dialogContentVariants = cva(
  "border-overlay-border bg-overlay-bg shadow-overlay fixed left-1/2 z-50 flex -translate-x-1/2 flex-col border outline-none",
  {
    defaultVariants: { kind: "dialog" },
    variants: {
      kind: {
        // Centred, `dialog.width`, `dialog.padding` and `dialog.gap` between its blocks.
        dialog:
          "rounded-dialog w-dialog p-dialog gap-dialog top-1/2 -translate-y-1/2",
        // The palette: `palette.top` from the window top, `palette.width`, no padding of its own.
        palette: "rounded-palette top-palette w-palette overflow-hidden",
      },
    },
  }
);

const DialogContent = ({
  kind,
  children,
  ...props
}: OwnProps<typeof DialogPrimitive.Content> &
  VariantProps<typeof dialogContentVariants>) => (
  <DialogPortal>
    <DialogOverlay />
    <DialogPrimitive.Content
      data-slot="dialog-content"
      className={dialogContentVariants({ kind })}
      {...props}
    >
      {children}
    </DialogPrimitive.Content>
  </DialogPortal>
);

/** The title and the description, with a ghost Close at the top right. */
const DialogHeader = ({
  showCloseButton = true,
  children,
}: {
  showCloseButton?: boolean;
  children: ReactNode;
}) => (
  <div data-slot="dialog-header" className="flex items-start gap-3">
    <div className="flex min-w-0 flex-1 flex-col gap-1">{children}</div>
    {showCloseButton && (
      <DialogClose asChild>
        <Button variant="ghost" size="icon" aria-label="Close">
          <CloseIcon />
        </Button>
      </DialogClose>
    )}
  </div>
);

const DialogFooter = ({ ...props }: OwnProps<"div">) => (
  <div
    data-slot="dialog-footer"
    className="flex justify-end gap-2"
    {...props}
  />
);

const DialogTitle = ({ ...props }: OwnProps<typeof DialogPrimitive.Title>) => (
  <DialogPrimitive.Title
    data-slot="dialog-title"
    className="type-dialog-title text-dialog-title m-0"
    {...props}
  />
);

const DialogDescription = ({
  ...props
}: OwnProps<typeof DialogPrimitive.Description>) => (
  <DialogPrimitive.Description
    data-slot="dialog-description"
    className="type-secondary text-dialog-subtitle m-0"
    {...props}
  />
);

export {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogOverlay,
  DialogPortal,
  DialogTitle,
  DialogTrigger,
};
