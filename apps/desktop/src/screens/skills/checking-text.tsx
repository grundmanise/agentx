import type { ReactNode } from "react";

import { InfoIcon } from "#/components/icons/info.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";

/** What the info icon of the Check updates toast says: how many sources the check went through. */
export const sourcesText = (current?: number, total?: number) => {
  if (current === undefined || total === undefined) {
    return "Determining the number of sources";
  }
  return `Checked ${current} of ${total} ${total === 1 ? "source" : "sources"}`;
};

/**
 * The text of the Check updates toast: "Checking for updates" and an info icon, whose tooltip
 * says how many sources the check went through, or that it does not know yet. The text stays the
 * same as the count comes, so the toast, centred on the window, keeps its width and place.
 */
export const checkingText = (current?: number, total?: number): ReactNode => (
  <span className="inline-flex items-center gap-1.5">
    Checking for updates
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="plain" size="inline" aria-label="Sources">
          <InfoIcon size="xs" />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{sourcesText(current, total)}</TooltipContent>
    </Tooltip>
  </span>
);
