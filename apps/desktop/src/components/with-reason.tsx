import type { ReactElement } from "react";

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";

/**
 * A control with the reason it does nothing in a Tooltip, or the control alone without one. The
 * control stays focusable, so the keyboard reaches the Tooltip too.
 */
export const WithReason = ({
  reason,
  children,
}: {
  reason: string | undefined;
  children: ReactElement;
}) => {
  if (reason === undefined) {
    return children;
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent>{reason}</TooltipContent>
    </Tooltip>
  );
};
