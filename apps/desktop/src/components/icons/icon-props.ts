import type { ReactElement } from "react";

/** Rendered sizes DESIGN.md §7 allows. */
export type IconSize = 13 | 14 | 15 | 16 | 18;

/**
 * The one prop every icon takes. An icon draws in `currentColor`, so its colour is the text colour
 * of its parent; it takes no colour, `className` or `style`.
 */
export interface IconProps {
  size?: IconSize;
}

/** Any icon in this folder, for lists that give each row its own icon (sidebar, palette). */
export type IconComponent = (props: IconProps) => ReactElement;
