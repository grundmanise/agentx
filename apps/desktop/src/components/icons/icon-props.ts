import type { ReactElement } from "react";

/** The icon size scale (DESIGN.md §7), in pixels. */
export const iconSizes = {
  lg: 18,
  md: 16,
  sm: 15,
  xs: 14,
} as const;

export type IconSize = keyof typeof iconSizes;

export interface IconProps {
  size?: IconSize;
}

/** Any icon, for lists that give each row its own icon (sidebar, palette). */
export type IconComponent = (props: IconProps) => ReactElement;
