// shadcn/ui Button (`pnpm dlx shadcn@latest add button`), on our tokens: DESIGN.md §6 "Buttons".
// It keeps shadcn's shape (`asChild` renders the child element with the button's look, through
// Slot; `data-slot="button"`) with our variants and sizes. Disabled is 30% opacity and inert,
// never a grey fill. A button is one line, except `sm-wrap`: a small button whose label wraps, as
// tall as its lines, for a label that is a sentence (a commit's subject in the History sheet).
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Slot } from "radix-ui";

import type { OwnProps } from "#/lib/utils.ts";

const buttonVariants = cva(
  "rounded-button inline-flex shrink-0 gap-2 transition-opacity duration-200 disabled:pointer-events-none disabled:opacity-30",
  {
    defaultVariants: { size: "default", variant: "secondary" },
    variants: {
      size: {
        default:
          "type-button h-9.5 items-center justify-center px-4 whitespace-nowrap",
        icon: "type-button size-8 items-center justify-center p-0 whitespace-nowrap",
        // A square as tall as a small button, as shadcn pairs `icon-sm` with `sm`.
        "icon-sm":
          "type-button-sm size-8.5 items-center justify-center p-0 whitespace-nowrap",
        sm: "type-button-sm h-8.5 items-center justify-center px-3.5 whitespace-nowrap",
        "sm-wrap":
          "type-button-sm min-h-8.5 items-start justify-start px-3.5 py-1.5 text-left whitespace-normal",
      },
      variant: {
        danger:
          "text-button-danger-text bg-button-danger-bg hover:bg-button-danger-hover font-medium",
        ghost:
          "text-button-ghost-text bg-button-ghost-bg hover:bg-button-ghost-hover hover:text-text-primary font-medium",
        primary:
          "bg-button-primary-bg text-button-primary-text hover:bg-button-primary-hover font-semibold",
        secondary:
          "bg-button-secondary-bg text-button-secondary-text hover:bg-button-secondary-hover font-medium",
      },
    },
  }
);

/** Style comes only from `variant` and `size`: Button takes no `className` or `style`. */
export type ButtonProps = Omit<OwnProps<"button">, "type"> &
  VariantProps<typeof buttonVariants> & {
    type?: "button" | "submit";
    /** Renders the child element (a link, a trigger's child) with the button's look instead of a `<button>`. */
    asChild?: boolean;
  };

const Button = ({
  variant,
  size,
  type = "button",
  asChild = false,
  ...props
}: ButtonProps) =>
  asChild ? (
    <Slot.Root
      data-slot="button"
      className={buttonVariants({ size, variant })}
      {...props}
    />
  ) : (
    <button
      data-slot="button"
      type={type === "submit" ? "submit" : "button"}
      className={buttonVariants({ size, variant })}
      {...props}
    />
  );

export { Button, buttonVariants };
