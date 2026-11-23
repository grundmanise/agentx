// shadcn/ui Tooltip (`pnpm dlx shadcn@latest add tooltip`), on our tokens: DESIGN.md §6
// "Tooltip". Its parts keep shadcn's names and `data-slot` attributes and take no `className`;
// its size and placement are variants. Its animation classes and its arrow are dropped (DESIGN.md
// §8), as no overlay of the app points at its anchor. `TooltipProvider` wraps the app once, in `src/main.tsx`.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Tooltip as TooltipPrimitive } from "radix-ui";

import type { OwnProps, PlacedProps } from "#/lib/utils.ts";

/** Shows a tooltip as soon as its trigger is pointed at or focused, as shadcn's does. */
const TooltipProvider = ({
  delayDuration = 0,
  ...props
}: OwnProps<typeof TooltipPrimitive.Provider>) => (
  <TooltipPrimitive.Provider
    data-slot="tooltip-provider"
    delayDuration={delayDuration}
    {...props}
  />
);

const Tooltip = ({ ...props }: OwnProps<typeof TooltipPrimitive.Root>) => (
  <TooltipPrimitive.Root data-slot="tooltip" {...props} />
);

const TooltipTrigger = ({
  ...props
}: OwnProps<typeof TooltipPrimitive.Trigger>) => (
  <TooltipPrimitive.Trigger data-slot="tooltip-trigger" {...props} />
);

const tooltipContentVariants = cva(
  "rounded-tooltip border-overlay-border bg-overlay-bg type-secondary text-text-primary z-50 w-fit border px-3 py-1.5 text-balance",
  {
    defaultVariants: { size: "line" },
    variants: {
      size: {
        // One line, as wide as its text.
        line: "",
        // A sentence that explains a term: it wraps past `tooltip.text-max-width`.
        text: "max-w-tooltip-text",
      },
    },
  }
);

// Where the tooltip opens: above its trigger, centred on it, or aligned to its start or its end.
const tooltipPlacements = {
  above: { align: "center", side: "top" },
  "above-end": { align: "end", side: "top" },
  "above-start": { align: "start", side: "top" },
} as const;

/**
 * `tooltip.radius`, `overlay.bg` with `overlay.border`, one `type.secondary` `text.primary` line
 * with shadcn's padding, `tooltip.offset` above its trigger (`placement`); `size="text"` wraps a
 * sentence.
 */
const TooltipContent = ({
  placement = "above",
  size,
  ...props
}: PlacedProps<typeof TooltipPrimitive.Content> &
  VariantProps<typeof tooltipContentVariants> & {
    placement?: keyof typeof tooltipPlacements;
  }) => (
  <TooltipPrimitive.Portal>
    <TooltipPrimitive.Content
      data-slot="tooltip-content"
      {...tooltipPlacements[placement]}
      // tooltip.offset
      sideOffset={6}
      className={tooltipContentVariants({ size })}
      {...props}
    />
  </TooltipPrimitive.Portal>
);

export { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger };
