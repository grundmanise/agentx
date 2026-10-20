import type { IconProps } from "#/components/icons/icon-props.ts";

/** A plus in a circle, for Add. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const AddIcon = ({ size = 16 }: IconProps) => (
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
    <path d="M12 8v8M8 12h8M12 3a9 9 0 1 0 0 18 9 9 0 1 0 0-18z" />
  </svg>
);
