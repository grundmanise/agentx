import { iconSizes } from "#/components/icons/icon-props.ts";
import type { IconProps } from "#/components/icons/icon-props.ts";

/** A folder, for showing a file where it is on disk. Stroke icon on a 24 grid in the parent's text colour (DESIGN.md §7). */
export const FolderIcon = ({ size = "md" }: IconProps) => (
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
    <path d="M3 7.5A1.5 1.5 0 0 1 4.5 6H9l2 2.5h8.5A1.5 1.5 0 0 1 21 10v7.5a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 17.5z" />
  </svg>
);
