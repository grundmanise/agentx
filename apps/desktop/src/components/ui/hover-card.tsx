// shadcn/ui Hover Card (`pnpm dlx shadcn@latest add hover-card`), on our tokens: DESIGN.md §6
// "Hover card". Its parts keep shadcn's names and `data-slot` attributes and take no `className`;
// its animation classes are dropped (DESIGN.md §8), and its frame is the popover's.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { HoverCard as HoverCardPrimitive } from "radix-ui";

import type { OwnProps } from "#/lib/utils.ts";

/** Opens `hover-card.open-delay` after the pointer reaches its trigger, closes `hover-card.close-delay` after it leaves. */
const HoverCard = ({
  // hover-card.open-delay
  openDelay = 200,
  // hover-card.close-delay
  closeDelay = 300,
  ...props
}: OwnProps<typeof HoverCardPrimitive.Root>) => (
  <HoverCardPrimitive.Root
    data-slot="hover-card"
    openDelay={openDelay}
    closeDelay={closeDelay}
    {...props}
  />
);

const HoverCardTrigger = ({
  ...props
}: OwnProps<typeof HoverCardPrimitive.Trigger>) => (
  <HoverCardPrimitive.Trigger data-slot="hover-card-trigger" {...props} />
);

const hoverCardContentVariants = cva(
  "rounded-popover border-overlay-border bg-overlay-bg shadow-popover w-popover max-h-hover-card gap-popover z-50 flex flex-col overflow-y-auto border outline-none",
  {
    defaultVariants: { padding: "all" },
    variants: {
      padding: {
        // 14 on every side.
        all: "p-3.5",
        // No top or bottom padding: a list that scrolls runs to the card's edges, its own
        // padding at the start and the end of the scroll.
        list: "px-3.5",
      },
    },
  }
);

/**
 * `popover.width`, `popover.radius`, `overlay.bg` with `overlay.border` and `popover.shadow`, 14
 * padding with `popover.gap` between its blocks; below its trigger, aligned to its start,
 * `popover.offset` away and `popover.window-inset` from the window's edges. No taller than
 * `hover-card.max-height`, the room the window has for it: past that it scrolls. `padding="list"`
 * leaves the top and bottom open for a list that scrolls to the edges.
 */
const HoverCardContent = ({
  align = "start",
  // popover.offset
  sideOffset = 8,
  // popover.window-inset
  collisionPadding = 10,
  padding,
  ...props
}: OwnProps<typeof HoverCardPrimitive.Content> &
  VariantProps<typeof hoverCardContentVariants>) => (
  <HoverCardPrimitive.Portal data-slot="hover-card-portal">
    <HoverCardPrimitive.Content
      data-slot="hover-card-content"
      align={align}
      sideOffset={sideOffset}
      collisionPadding={collisionPadding}
      className={hoverCardContentVariants({ padding })}
      {...props}
    />
  </HoverCardPrimitive.Portal>
);

export { HoverCard, HoverCardContent, HoverCardTrigger };
