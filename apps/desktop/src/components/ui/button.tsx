import { cva } from 'class-variance-authority';
import type { VariantProps } from 'class-variance-authority';
import type { ComponentProps } from "react";

// DESIGN.md §6 "Buttons". Disabled is 30% opacity and inert, never a grey fill.
const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-button transition-opacity duration-200 disabled:pointer-events-none disabled:opacity-30",
  {
    defaultVariants: { size: "default", variant: "secondary" },
    variants: {
      size: {
        default: "h-9.5 px-4 type-button",
        icon: "size-8 p-0 type-button",
        sm: "h-8.5 px-3.5 type-button-sm",
      },
      variant: {
        danger: "bg-transparent font-medium text-button-danger-text hover:bg-button-danger-hover",
        ghost: "bg-transparent font-medium text-button-ghost-text hover:bg-button-ghost-hover hover:text-text-primary",
        primary: "bg-button-primary-bg font-semibold text-button-primary-text hover:bg-button-primary-hover",
        secondary: "bg-button-secondary-bg font-medium text-button-secondary-text hover:bg-button-secondary-hover",
      },
    },
  },
);

/** Style comes only from `variant` and `size`: Button takes no `className` or `style`. */
export type ButtonProps = Omit<ComponentProps<"button">, "className" | "style" | "type"> &
  VariantProps<typeof buttonVariants> & { type?: "button" | "submit" };

export const Button = ({ variant, size, type = "button", ...props }: ButtonProps) => 
  <button type={type === "submit" ? "submit" : "button"} {...props} className={buttonVariants({ size, variant })} />
;
