import { iconSizes } from "#/components/icons/icon-props.ts";
import type { IconProps } from "#/components/icons/icon-props.ts";

/** Three narrowing lines, for the Filter button. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const FilterIcon = ({ size = "md" }: IconProps) => (
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
    <path d="M4 6h16M7 12h10M10 18h4" />
  </svg>
);
