import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";

// DESIGN.md §6 "Buttons". Disabled is 30% opacity and inert, never a grey fill.
const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-button transition-opacity duration-200 disabled:pointer-events-none disabled:opacity-30",
  {
    variants: {
      variant: {
        primary: "bg-button-primary-bg font-semibold text-button-primary-text hover:bg-button-primary-hover",
        secondary: "bg-button-secondary-bg font-medium text-button-secondary-text hover:bg-button-secondary-hover",
        ghost: "bg-transparent font-medium text-button-ghost-text hover:bg-button-ghost-hover hover:text-text-primary",
        danger: "bg-transparent font-medium text-button-danger-text hover:bg-button-danger-hover",
      },
      size: {
        default: "h-9.5 px-4 type-button",
        sm: "h-8.5 px-3.5 type-button-sm",
        icon: "size-8 p-0 type-button",
      },
    },
    defaultVariants: { variant: "secondary", size: "default" },
  },
);

/** Style comes only from `variant` and `size`: Button takes no `className` or `style`. */
export type ButtonProps = Omit<ComponentProps<"button">, "className" | "style"> & VariantProps<typeof buttonVariants>;

export function Button({ variant, size, type = "button", ...props }: ButtonProps) {
  return <button type={type} {...props} className={buttonVariants({ variant, size })} />;
}
