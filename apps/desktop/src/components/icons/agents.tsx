import type { IconProps } from "#/components/icons/icon-props.ts";

/** A robot head, for Agents. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const AgentsIcon = ({ size = 16 }: IconProps) => (
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
    <path d="M12 8V4H8M4 8h16v12H4zM2 14h2M20 14h2M9 13v2M15 13v2" />
  </svg>
);
