import { clsx } from "clsx";
import type { ClassValue } from "clsx";
import type { ComponentProps, ElementType } from "react";
import { twMerge } from "tailwind-merge";

export const cn = (...inputs: ClassValue[]) => twMerge(clsx(inputs));

/**
 * The props of an element or component without `className` and `style`: what a component of
 * ours takes (README "Rules for components"). Its look comes from its variants.
 */
export type OwnProps<T extends ElementType> = Omit<
  ComponentProps<T>,
  "className" | "style"
>;
