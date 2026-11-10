// shadcn/ui Tooltip (`pnpm dlx shadcn@latest add tooltip`), on our tokens: DESIGN.md §6
// "Tooltip". Its parts keep shadcn's names and `data-slot` attributes and take no `className`;
// its animation classes and its arrow are dropped (DESIGN.md §8). Each Tooltip brings its own
// provider, so a screen needs no provider around it.
import { Tooltip as TooltipPrimitive } from "radix-ui";

import type { OwnProps } from "#/lib/utils.ts";

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
  <TooltipProvider>
    <TooltipPrimitive.Root data-slot="tooltip" {...props} />
  </TooltipProvider>
);

const TooltipTrigger = ({
  ...props
}: OwnProps<typeof TooltipPrimitive.Trigger>) => (
  <TooltipPrimitive.Trigger data-slot="tooltip-trigger" {...props} />
);

/**
 * `overlay.bg` with `overlay.border`, `tooltip.radius`, `type.secondary` in `text.primary`,
 * `tooltip.offset` below its trigger (above when there is no room).
 */
const TooltipContent = ({
  side = "bottom",
  // tooltip.offset
  sideOffset = 6,
  children,
  ...props
}: OwnProps<typeof TooltipPrimitive.Content>) => (
  <TooltipPrimitive.Portal>
    <TooltipPrimitive.Content
      data-slot="tooltip-content"
      side={side}
      sideOffset={sideOffset}
      className="rounded-tooltip border-overlay-border bg-overlay-bg type-secondary text-text-primary z-50 w-fit border px-2.5 py-1 text-balance"
      {...props}
    >
      {children}
    </TooltipPrimitive.Content>
  </TooltipPrimitive.Portal>
);

export { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger };
