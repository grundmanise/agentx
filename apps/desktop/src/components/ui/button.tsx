// shadcn/ui Button (`pnpm dlx shadcn@latest add button`), on our tokens: DESIGN.md §6 "Buttons".
// It keeps shadcn's shape (`asChild` renders the child element with the button's look, through
// Slot; `data-slot="button"`) with our variants and sizes. Disabled is 30% opacity and inert,
// never a grey fill; the filter row's toggles fade to 50%, like a pending segmented control.
// `aria-disabled` looks the same but stays hoverable and focusable, for a button whose Tooltip
// says why it does nothing. A button is one line, except `sm-wrap`: a small button whose label
// wraps, as tall as its lines, for a label that is a sentence, and `inline`, a `plain` button as
// tall as its text (a commit's subject in the History sheet).
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Slot } from "radix-ui";

import type { OwnProps } from "#/lib/utils.ts";

const buttonVariants = cva(
  "rounded-button inline-flex shrink-0 gap-2 transition-opacity duration-200 disabled:pointer-events-none",
  {
    compoundVariants: [
      {
        className: "disabled:opacity-30 aria-disabled:opacity-30",
        variant: [
          "accent",
          "danger",
          "filter",
          "filter-active",
          "filter-chip",
          "ghost",
          "link",
          "plain",
          "primary",
          "secondary",
        ],
      },
      { className: "disabled:opacity-50", variant: "filter-toggle" },
    ],
    defaultVariants: { size: "default", variant: "secondary" },
    variants: {
      size: {
        // The cross of a filter chip, inside the chip.
        chip: "size-5.5 items-center justify-center p-0",
        default:
          "type-button h-9.5 items-center justify-center px-4 whitespace-nowrap",
        // The Filter button, `filter.height` tall.
        filter:
          "type-button-sm h-filter items-center justify-center px-3 whitespace-nowrap",
        // The expand toggle of the filter row: a 28px square.
        "filter-icon": "size-7 items-center justify-center p-0",
        // The sort toggle of the filter row.
        "filter-toggle":
          "type-button-sm h-7 items-center justify-center px-2 whitespace-nowrap",
        icon: "type-button size-8 items-center justify-center p-0 whitespace-nowrap",
        // A square as tall as a small button, as shadcn pairs `icon-sm` with `sm`.
        "icon-sm":
          "type-button-sm size-8.5 items-center justify-center p-0 whitespace-nowrap",
        // As tall as its text, with no padding: a plain button inside a line of text, such as an
        // entry of the History sheet, whose children set its type.
        inline: "items-start justify-start p-0 text-left whitespace-normal",
        sm: "type-button-sm h-8.5 items-center justify-center px-3.5 whitespace-nowrap",
        "sm-wrap":
          "type-button-sm min-h-8.5 items-start justify-start px-3.5 py-1.5 text-left whitespace-normal",
      },
      variant: {
        // The plain inline shape in the accent: the call to action at the end of a list's group
        // header, such as Adopt all.
        accent:
          "text-accent-primary hover:text-accent-primary-hover font-medium",
        danger:
          "text-button-danger-text bg-button-danger-bg hover:bg-button-danger-hover font-medium",
        // The Filter button (DESIGN.md §2.2 "Filter row"): `filter.bg` at rest, `filter.active-bg`
        // in `filter.active-text` while any filter is applied.
        filter:
          "bg-filter-bg text-filter-text hover:text-text-primary font-medium",
        "filter-active":
          "bg-filter-active-bg text-filter-active-text font-medium",
        // The cross that clears a filter chip: the chip's text, `filter.chip-hover` on hover.
        "filter-chip": "hover:bg-filter-chip-hover hover:text-text-primary",
        // The sort and expand toggles of the filter row: `filter.sort-text` on the ghost fill.
        "filter-toggle":
          "text-filter-sort-text bg-button-ghost-bg hover:bg-button-ghost-hover hover:text-text-primary font-medium",
        ghost:
          "text-button-ghost-text bg-button-ghost-bg hover:bg-button-ghost-hover hover:text-text-primary font-medium",
        // A link inside a line of text that opens something, such as See changes on a row of the
        // Update skills dialog: underlined on hover.
        link: "text-text-secondary hover:text-text-primary font-medium hover:underline",
        // No fill: a line of text that expands, such as an entry of the History sheet, or, small, a
        // toast's action, such as Undo.
        plain: "text-button-ghost-text hover:text-text-primary",
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
