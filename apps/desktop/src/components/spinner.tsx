/**
 * A turning ring in the parent's text colour, for a button whose action runs: one rotation a
 * second, linear (DESIGN.md §8). Show it only while something runs; nothing turns forever.
 */
export const Spinner = () => (
  <svg
    viewBox="0 0 20 20"
    fill="none"
    aria-hidden
    focusable="false"
    className="animate-check-spin block size-4 flex-none"
  >
    <path
      d="M10 1.5a8.5 8.5 0 0 1 8.5 8.5"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
    />
  </svg>
);
