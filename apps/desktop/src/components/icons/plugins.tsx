import type { IconProps } from "#/components/icons/icon-props.ts";

/** A plug, for Plugins. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const PluginsIcon = ({ size = 16 }: IconProps) => (
  <svg
    width={size}
    height={size}
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
    <path d="M9 3v4M15 3v4M6 7h12v4a6 6 0 0 1-12 0zM12 17v4" />
  </svg>
);
