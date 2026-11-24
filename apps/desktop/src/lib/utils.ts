import type { ComponentProps, ElementType } from "react";

/** Joins classes like clsx and drops the ones a later class overrides, like tailwind-merge. */
export { cn } from "cn";

/**
 * The props of an element or component without `className` and `style`: what a component of
 * ours takes (README "Rules for components"). Its look comes from its variants.
 */
export type OwnProps<T extends ElementType> = Omit<
  ComponentProps<T>,
  "className" | "style"
>;

/**
 * The props of a Radix overlay's content without its placement (`side`, `align`, their offsets and
 * `collisionPadding`): where it opens is a closed `placement` variant of ours, and its gaps come
 * from tokens (README "Rules for components").
 */
export type PlacedProps<T extends ElementType> = Omit<
  OwnProps<T>,
  "side" | "align" | "sideOffset" | "alignOffset" | "collisionPadding"
>;
