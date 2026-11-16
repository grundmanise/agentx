// shadcn/ui Tooltip (`pnpm dlx shadcn@latest add tooltip`), on our tokens: DESIGN.md §6
// "Tooltip". Its parts keep shadcn's names and `data-slot` attributes and take no `className`;
// its animation classes and its arrow are dropped (DESIGN.md §8), as no overlay of the app
// points at its anchor. `TooltipProvider` wraps the app once, in `src/main.tsx`.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Tooltip as TooltipPrimitive } from "radix-ui";

import type { OwnProps } from "#/lib/utils.ts";

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

/**
 * `tooltip.radius`, `overlay.bg` with `overlay.border`, one `type.secondary` `text.primary` line
 * with shadcn's padding, `tooltip.offset` from its trigger; `size="text"` wraps a sentence.
 */
const TooltipContent = ({
  // tooltip.offset
  sideOffset = 6,
  size,
  ...props
}: OwnProps<typeof TooltipPrimitive.Content> &
  VariantProps<typeof tooltipContentVariants>) => (
  <TooltipPrimitive.Portal>
    <TooltipPrimitive.Content
      data-slot="tooltip-content"
      sideOffset={sideOffset}
      className={tooltipContentVariants({ size })}
      {...props}
    />
  </TooltipPrimitive.Portal>
);

export { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger };
