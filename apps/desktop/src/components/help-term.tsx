import type { ReactNode } from "react";

import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";

/**
 * A term in running text that explains itself (DESIGN.md §6 "Tooltip"): the words keep the
 * text's type and colour with a dotted underline, and `tip` opens on hover and on focus, so the
 * term is a button the keyboard reaches. It does nothing on a click. The punctuation that follows
 * the term goes in `trailing`, inside the button and outside the underline: a button is an inline
 * block, so a screen reader would otherwise read a space before it.
 */
export const HelpTerm = ({
  tip,
  trailing,
  children,
}: {
  tip: ReactNode;
  trailing?: string;
  children: string;
}) => (
  <Tooltip>
    <TooltipTrigger asChild>
      <button type="button" className="cursor-help">
        <span className="underline decoration-dotted underline-offset-2">
          {children}
        </span>
        {trailing}
      </button>
    </TooltipTrigger>
    <TooltipContent size="text">{tip}</TooltipContent>
  </Tooltip>
);
