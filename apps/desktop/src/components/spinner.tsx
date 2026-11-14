import { cva } from "class-variance-authority";

const spinnerVariants = cva("animate-check-spin block flex-none", {
  defaultVariants: { size: 16 },
  variants: {
    size: {
      16: "size-4",
      20: "size-5",
    },
  },
});

/**
 * A turning ring in the parent's text colour, for a button whose action runs or a slow launch: one
 * rotation a second, linear (DESIGN.md §8). Show it only while something runs; nothing turns forever.
 * 16px in a control; 20px alone over a pane, such as the files being read.
 */
export const Spinner = ({ size }: { size?: 16 | 20 }) => (
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
