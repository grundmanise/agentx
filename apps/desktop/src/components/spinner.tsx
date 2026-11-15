import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";

const spinnerVariants = cva("animate-check-spin block flex-none", {
  defaultVariants: { size: "sm" },
  variants: {
    size: {
      md: "size-5",
      sm: "size-4",
    },
  },
});

/**
 * A turning ring in the parent's text colour, for a button whose action runs or a launch: one
 * rotation a second, linear (DESIGN.md §8). Show it only while something runs; nothing turns forever.
 * `sm` (16px) in a control; `md` (20px) alone over a pane, such as the files being read.
 */
export const Spinner = ({
  size,
}: Pick<VariantProps<typeof spinnerVariants>, "size">) => (
  <svg
    viewBox="0 0 20 20"
    fill="none"
    aria-hidden
    focusable="false"
    className={spinnerVariants({ size })}
  >
    <path
      d="M10 1.5a8.5 8.5 0 0 1 8.5 8.5"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
    />
  </svg>
);
