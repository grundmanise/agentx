// shadcn/ui Popover (`pnpm dlx shadcn@latest add popover`), on our tokens: DESIGN.md §6
// "Popover". Its parts keep shadcn's names and `data-slot` attributes and take no `className`;
// the content's padding is a variant.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Popover as PopoverPrimitive } from "radix-ui";
import type { ReactNode } from "react";

import type { OwnProps } from "#/lib/utils.ts";

const Popover = ({ ...props }: OwnProps<typeof PopoverPrimitive.Root>) => (
  <PopoverPrimitive.Root data-slot="popover" {...props} />
);

const PopoverTrigger = ({
  ...props
}: OwnProps<typeof PopoverPrimitive.Trigger>) => (
  <PopoverPrimitive.Trigger data-slot="popover-trigger" {...props} />
);

const PopoverAnchor = ({
  ...props
}: OwnProps<typeof PopoverPrimitive.Anchor>) => (
  <PopoverPrimitive.Anchor data-slot="popover-anchor" {...props} />
);

// `popover.width`, `popover.radius`, `overlay.bg` with `overlay.border` and `popover.shadow`. No
// taller than `popover.max-height`, the room the window has for it: past that it scrolls. It opens
// and closes with shadcn's own classes (DESIGN.md §8), and takes no clicks while it closes.
const popoverContentVariants = cva(
  "rounded-popover border-overlay-border bg-overlay-bg shadow-popover max-h-popover data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2 origin-popover z-40 flex flex-col overflow-y-auto border outline-none data-[state=closed]:pointer-events-none!",
  {
    defaultVariants: { padding: "groups", width: "fixed" },
    variants: {
      // `popover.padding`: 14 for grouped content, with `popover.gap` between the groups; 8 for
      // a list of rows.
      padding: {
        groups: "gap-popover p-3.5",
        rows: "p-2",
      },
      // `popover.width`; with `fit`, at least that and as wide as its content needs, up to
      // `popover.max-width`, so a line that must show in full, such as the sync popover's account
      // remote, widens it.
      width: {
        fit: "min-w-popover max-w-popover w-max",
        fixed: "w-popover",
      },
    },
  }
);

/** Below its anchor, aligned to its start, `popover.offset` away, `popover.window-inset` from the window's edges. */
const PopoverContent = ({
  padding,
  width,
  align = "start",
  // popover.offset
  sideOffset = 8,
  // popover.window-inset
  collisionPadding = 10,
  ...props
}: OwnProps<typeof PopoverPrimitive.Content> &
  VariantProps<typeof popoverContentVariants>) => (
  <PopoverPrimitive.Portal>
    <PopoverPrimitive.Content
      data-slot="popover-content"
      align={align}
      sideOffset={sideOffset}
      collisionPadding={collisionPadding}
      className={popoverContentVariants({ padding, width })}
      {...props}
    />
  </PopoverPrimitive.Portal>
);

const PopoverHeader = ({ ...props }: OwnProps<"div">) => (
  <div data-slot="popover-header" className="flex flex-col gap-1" {...props} />
);

const PopoverTitle = ({
  children,
  ...props
}: OwnProps<"h2"> & { children: ReactNode }) => (
  <h2
    data-slot="popover-title"
    className="type-secondary text-text-primary m-0 font-medium"
    {...props}
  >
    {children}
  </h2>
);

const PopoverDescription = ({ ...props }: OwnProps<"p">) => (
  <p
    data-slot="popover-description"
    className="type-secondary text-text-muted m-0"
    {...props}
  />
);

export {
  Popover,
  PopoverAnchor,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
};
