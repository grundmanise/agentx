import * as RadixPopover from "@radix-ui/react-popover";
import type { ReactNode } from "react";

/**
 * A popover anchored below its trigger (DESIGN.md §2.2 "Overlays"): `overlay.bg`, `overlay.border`,
 * `popover.shadow`, `popover.width`, 14px padding for grouped content and `popover.gap` between
 * groups. A click outside or Escape closes it.
 */
export const Popover = ({
  open,
  onOpenChange,
  trigger,
  label,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  trigger: ReactNode;
  /** The accessible name of the popover. */
  label: string;
  children: ReactNode;
}) => (
  <RadixPopover.Root open={open} onOpenChange={onOpenChange}>
    <RadixPopover.Trigger asChild>{trigger}</RadixPopover.Trigger>
    <RadixPopover.Portal>
      <RadixPopover.Content
        aria-label={label}
        align="start"
        // popover.offset
        sideOffset={8}
        className="rounded-popover border-overlay-border bg-overlay-bg shadow-popover w-popover gap-popover z-40 flex flex-col border p-3.5 outline-none"
      >
        {children}
      </RadixPopover.Content>
    </RadixPopover.Portal>
  </RadixPopover.Root>
);
