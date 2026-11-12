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
// taller than `popover.max-height`, the room the window has for it: past that it scrolls.
const popoverContentVariants = cva(
  "rounded-popover border-overlay-border bg-overlay-bg shadow-popover w-popover max-h-popover z-40 flex flex-col overflow-y-auto border outline-none",
  {
    defaultVariants: { padding: "groups" },
    variants: {
      // `popover.padding`: 14 for grouped content, with `popover.gap` between the groups; 8 for
      // a list of rows.
      padding: {
        groups: "gap-popover p-3.5",
        rows: "p-2",
      },
    },
  }
);

/** Below its anchor, aligned to its start, `popover.offset` away, `popover.window-inset` from the window's edges. */
const PopoverContent = ({
  padding,
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
      className={popoverContentVariants({ padding })}
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
