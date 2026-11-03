import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";

// DESIGN.md §6 "Buttons". Disabled is 30% opacity and inert, never a grey fill. A button is one
// line, except `sm-wrap`: a small button whose label wraps, as tall as its lines, for a label that
// is a sentence (a commit's subject in the History sheet).
const buttonVariants = cva(
  "rounded-button inline-flex shrink-0 gap-2 transition-opacity duration-200 disabled:pointer-events-none disabled:opacity-30",
  {
    defaultVariants: { size: "default", variant: "secondary" },
    variants: {
      size: {
        default:
          "type-button h-9.5 items-center justify-center px-4 whitespace-nowrap",
        icon: "type-button size-8 items-center justify-center p-0 whitespace-nowrap",
        sm: "type-button-sm h-8.5 items-center justify-center px-3.5 whitespace-nowrap",
        "sm-wrap":
          "type-button-sm min-h-8.5 items-start justify-start px-3.5 py-1.5 text-left whitespace-normal",
      },
      variant: {
        danger:
          "text-button-danger-text hover:bg-button-danger-hover bg-transparent font-medium",
        ghost:
          "text-button-ghost-text hover:bg-button-ghost-hover hover:text-text-primary bg-transparent font-medium",
        primary:
          "bg-button-primary-bg text-button-primary-text hover:bg-button-primary-hover font-semibold",
        secondary:
          "bg-button-secondary-bg text-button-secondary-text hover:bg-button-secondary-hover font-medium",
      },
    },
  }
);

/** Style comes only from `variant` and `size`: Button takes no `className` or `style`. */
export type ButtonProps = Omit<
  ComponentProps<"button">,
  "className" | "style" | "type"
> &
  VariantProps<typeof buttonVariants> & { type?: "button" | "submit" };

export const Button = ({
  variant,
  size,
  type = "button",
  ...props
}: ButtonProps) => (
  <button
    type={type === "submit" ? "submit" : "button"}
    className={buttonVariants({ size, variant })}
    {...props}
  />
);
