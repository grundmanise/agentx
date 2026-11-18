import { iconSizes } from "#/components/icons/icon-props.ts";
import type { IconProps } from "#/components/icons/icon-props.ts";

/** A check in a circle, for a toast whose command succeeded. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const CircleCheckIcon = ({ size = "md" }: IconProps) => (
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
    <path d="M12 3a9 9 0 1 0 0 18 9 9 0 1 0 0-18zM8.5 12.2l2.4 2.4 4.6-4.9" />
  </svg>
);
