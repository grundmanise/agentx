import type { ReactElement } from "react";

/** The icon sizes DESIGN.md §7 allows: each name says where the icon sits, the value is its pixels. */
export const iconSizes = {
  /** The search icon in the title bar's command field. */
  field: 14,
  /** Sidebar nav items, and the default. */
  nav: 16,
  /** Palette rows. */
  row: 15,
  /** The search icon in the palette's input. */
  search: 18,
} as const;

export type IconSize = keyof typeof iconSizes;

/**
 * The one prop every icon takes. An icon draws in `currentColor`, so its colour is the text colour
 * of its parent; it takes no colour, `className` or `style`.
 */
export interface IconProps {
  size?: IconSize;
}

/** Any icon in this folder, for lists that give each row its own icon (sidebar, palette). */
export type IconComponent = (props: IconProps) => ReactElement;
