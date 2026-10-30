import { iconSizes } from "#/components/icons/icon-props.ts";
import type { IconProps } from "#/components/icons/icon-props.ts";

/** A circling arrow, for checking whether a newer agentx is out. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const UpdateIcon = ({ size = "md" }: IconProps) => (
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
    <path d="M20 12a8 8 0 1 1-2.34-5.66M20 4v5h-5" />
  </svg>
);
