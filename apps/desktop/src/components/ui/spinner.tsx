// shadcn/ui Spinner (`pnpm dlx shadcn@latest add spinner`), on our tokens: DESIGN.md §8. It keeps
// shadcn's shape, an `svg` that takes the svg's props, but no `className` or `style`: its look is
// its `size`. shadcn's lucide Loader2Icon is swapped for our ring, which turns with
// `animate-check-spin` (DESIGN.md §8). shadcn makes it a "Loading" status; ours stays hidden from
// assistive technology, since the control or region around it already says what runs.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";

import type { OwnProps } from "#/lib/utils.ts";

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
const Spinner = ({
  size,
  ...props
}: OwnProps<"svg"> & VariantProps<typeof spinnerVariants>) => (
  <svg
    viewBox="0 0 20 20"
    fill="none"
    aria-hidden
    focusable="false"
    className={spinnerVariants({ size })}
    {...props}
  >
    <path
      d="M10 1.5a8.5 8.5 0 0 1 8.5 8.5"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
    />
  </svg>
);

export { Spinner };
