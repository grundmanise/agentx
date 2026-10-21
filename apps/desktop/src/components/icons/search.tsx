import { iconSizes } from "#/components/icons/icon-props.ts";
import type { IconProps } from "#/components/icons/icon-props.ts";

/** A magnifier, for the command field and the palette. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const SearchIcon = ({ size = "md" }: IconProps) => (
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
    <path d="M11 4a7 7 0 1 0 0 14 7 7 0 1 0 0-14zM20 20l-3.5-3.5" />
  </svg>
);
