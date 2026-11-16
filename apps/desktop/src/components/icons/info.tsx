import { iconSizes } from "#/components/icons/icon-props.ts";
import type { IconProps } from "#/components/icons/icon-props.ts";

/** An "i" in a circle, for a control that tells more on hover. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const InfoIcon = ({ size = "md" }: IconProps) => (
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
    <path d="M12 3a9 9 0 1 0 0 18 9 9 0 1 0 0-18zM12 16v-5M12 7.5v.01" />
  </svg>
);
