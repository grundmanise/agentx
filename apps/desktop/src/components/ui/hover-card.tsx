// shadcn/ui Hover Card (`pnpm dlx shadcn@latest add hover-card`), on our tokens: DESIGN.md §6
// "Hover card". Its parts keep shadcn's names and `data-slot` attributes and take no `className`;
// its animation classes are dropped (DESIGN.md §8), its frame is the popover's, and its padding
// and placement are variants.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { HoverCard as HoverCardPrimitive } from "radix-ui";

import type { OwnProps, PlacedProps } from "#/lib/utils.ts";

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
  "rounded-popover border-overlay-border bg-overlay-bg shadow-popover max-h-hover-card gap-popover z-50 flex flex-col overflow-y-auto border outline-none",
  {
    defaultVariants: { padding: "all" },
    variants: {
      padding: {
        // 14 on every side.
        all: "w-popover p-3.5",
        // No top or bottom padding: a list that scrolls runs to the card's edges, its own
        // padding at the start and the end of the scroll. As wide as the list, so its right
        // padding matches its left.
        list: "max-w-hover-card w-max px-3.5",
      },
    },
  }
);

// Where the card opens: below its trigger, aligned to its start or its end; or to its left, from
// its top.
const hoverCardPlacements = {
  "below-end": { align: "end", side: "bottom" },
  "below-start": { align: "start", side: "bottom" },
  "left-start": { align: "start", side: "left" },
} as const;

/** Where a hover card opens: below its trigger, from its start or its end, or to its left. */
export type HoverCardPlacement = keyof typeof hoverCardPlacements;

/**
 * `popover.width`, `popover.radius`, `overlay.bg` with `overlay.border` and `popover.shadow`, 14
 * padding with `popover.gap` between its blocks; below its trigger by default (`placement`),
 * `popover.offset` away and `popover.window-inset` from the window's edges. No taller than
 * `hover-card.max-height`, the room the window has for it: past that it scrolls. `padding="list"`
 * leaves the top and bottom open for a list that scrolls to the edges, and fits the card to the
 * list's width, at most `popover.width`.
 */
const HoverCardContent = ({
  placement = "below-start",
  padding,
  ...props
}: PlacedProps<typeof HoverCardPrimitive.Content> &
  VariantProps<typeof hoverCardContentVariants> & {
    placement?: HoverCardPlacement;
  }) => (
  <HoverCardPrimitive.Portal data-slot="hover-card-portal">
    <HoverCardPrimitive.Content
      data-slot="hover-card-content"
      {...hoverCardPlacements[placement]}
      // popover.offset
      sideOffset={8}
      // popover.window-inset
      collisionPadding={10}
      className={hoverCardContentVariants({ padding })}
      {...props}
    />
  </HoverCardPrimitive.Portal>
);

export { HoverCard, HoverCardContent, HoverCardTrigger };
