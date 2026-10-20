import { iconSizes } from "#/components/icons/icon-props.ts";
import type { IconProps } from "#/components/icons/icon-props.ts";

/** Two server racks, for MCP servers. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const ServersIcon = ({ size = "nav" }: IconProps) => (
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
    <path d="M4 4h16v6H4zM4 14h16v6H4zM8 7h.01M8 17h.01" />
  </svg>
);
