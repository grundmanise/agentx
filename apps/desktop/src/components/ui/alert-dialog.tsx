// shadcn/ui Alert Dialog (`pnpm dlx shadcn@latest add alert-dialog`), on our tokens: a confirm
// before a destructive command, such as Remove, Replace, Detach or Abort. It looks and moves like
// the Dialog (DESIGN.md §2.2 "Overlays", §8), but it never closes on a click outside: the user
// picks Cancel, the action, or Escape. Its parts keep shadcn's names and `data-slot` attributes and
// take no `className`. Cancel and the action are our Buttons: Cancel through `AlertDialogCancel`,
// the action a plain Button that closes the dialog itself, so it can run a command first.
import { AlertDialog as AlertDialogPrimitive } from "radix-ui";
import type { ReactNode } from "react";

import {
  DIALOG_OVERLAY,
  dialogContentVariants,
} from "#/components/ui/dialog.tsx";
import type { OwnProps } from "#/lib/utils.ts";

const AlertDialog = ({
  ...props
}: OwnProps<typeof AlertDialogPrimitive.Root>) => (
  <AlertDialogPrimitive.Root data-slot="alert-dialog" {...props} />
);

const AlertDialogTrigger = ({
  ...props
}: OwnProps<typeof AlertDialogPrimitive.Trigger>) => (
  <AlertDialogPrimitive.Trigger data-slot="alert-dialog-trigger" {...props} />
);

const AlertDialogPortal = ({
  ...props
}: OwnProps<typeof AlertDialogPrimitive.Portal>) => (
  <AlertDialogPrimitive.Portal data-slot="alert-dialog-portal" {...props} />
);

/** `shell.scrim-strong`, as behind a Dialog. */
const AlertDialogOverlay = ({
  ...props
}: OwnProps<typeof AlertDialogPrimitive.Overlay>) => (
  <AlertDialogPrimitive.Overlay
    data-slot="alert-dialog-overlay"
    className={DIALOG_OVERLAY}
    {...props}
  />
);

/** Centred, like a Dialog's content. */
const AlertDialogContent = ({
  children,
  ...props
}: OwnProps<typeof AlertDialogPrimitive.Content>) => (
  <AlertDialogPortal>
    <AlertDialogOverlay />
    <AlertDialogPrimitive.Content
      data-slot="alert-dialog-content"
      className={dialogContentVariants({ kind: "dialog" })}
      {...props}
    >
      {children}
    </AlertDialogPrimitive.Content>
  </AlertDialogPortal>
);

/** The title and the description. There is no Close: Cancel closes it. */
const AlertDialogHeader = ({ children }: { children: ReactNode }) => (
  <div data-slot="alert-dialog-header" className="flex min-w-0 flex-col gap-1">
    {children}
  </div>
);

const AlertDialogFooter = ({ ...props }: OwnProps<"div">) => (
  <div
    data-slot="alert-dialog-footer"
    className="flex justify-end gap-2"
    {...props}
  />
);

const AlertDialogTitle = ({
  ...props
}: OwnProps<typeof AlertDialogPrimitive.Title>) => (
  <AlertDialogPrimitive.Title
    data-slot="alert-dialog-title"
    className="type-dialog-title text-dialog-title m-0"
    {...props}
  />
);

const AlertDialogDescription = ({
  ...props
}: OwnProps<typeof AlertDialogPrimitive.Description>) => (
  <AlertDialogPrimitive.Description
    data-slot="alert-dialog-description"
    className="type-secondary text-dialog-subtitle m-0"
    {...props}
  />
);

/** Wraps our ghost Button (`asChild`): it closes the dialog, and takes the focus as it opens. */
const AlertDialogCancel = ({
  ...props
}: OwnProps<typeof AlertDialogPrimitive.Cancel>) => (
  <AlertDialogPrimitive.Cancel data-slot="alert-dialog-cancel" {...props} />
);

export {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogOverlay,
  AlertDialogPortal,
  AlertDialogTitle,
  AlertDialogTrigger,
};
