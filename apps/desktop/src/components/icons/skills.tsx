import { iconSizes } from "#/components/icons/icon-props.ts";
import type { IconProps } from "#/components/icons/icon-props.ts";

/** Three stacked layers, for Skills. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const SkillsIcon = ({ size = "md" }: IconProps) => (
  <svg
    width={iconSizes[size]}
    height={iconSizes[size]}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.75}
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden
    focusable="false"
    className="block flex-none"
  >
    <path d="M12 3 3 8l9 5 9-5-9-5zM3 16l9 5 9-5M3 12l9 5 9-5" />
  </svg>
);
