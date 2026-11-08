import { iconSizes } from "#/components/icons/icon-props.ts";
import type { IconProps } from "#/components/icons/icon-props.ts";

/** An exclamation mark in a triangle, for a skill row whose banner warns. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const AlertTriangleIcon = ({ size = "md" }: IconProps) => (
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
    <path d="M12 3 2.5 20h19L12 3zM12 9.5v4.5M12 17v.01" />
  </svg>
);
