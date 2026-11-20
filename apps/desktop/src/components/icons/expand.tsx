import { iconSizes } from "#/components/icons/icon-props.ts";
import type { IconProps } from "#/components/icons/icon-props.ts";

/** Two chevrons pointing apart, for Expand all. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const ExpandIcon = ({ size = "md" }: IconProps) => (
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
    <path d="M7 15l5 5 5-5M7 9l5-5 5 5" />
  </svg>
);
