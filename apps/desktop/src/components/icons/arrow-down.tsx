import { iconSizes } from "#/components/icons/icon-props.ts";
import type { IconProps } from "#/components/icons/icon-props.ts";

/** An arrow pointing down, for the sort toggle, which turns it around when descending. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const ArrowDownIcon = ({ size = "md" }: IconProps) => (
  <svg
    width={iconSizes[size]}
    height={iconSizes[size]}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={2}
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden
    focusable="false"
    className="block flex-none"
  >
    <path d="M12 5v14M6 13l6 6 6-6" />
  </svg>
);
