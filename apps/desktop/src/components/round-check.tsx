/**
 * The 20px round check of a selectable card (DESIGN.md §6 "Round check"): a ring in
 * `round-check.ring`, filled with `round-check.bg` and an `on-accent` check when selected. It
 * draws inside the `Toggle` of `kind="check"` that holds it.
 */
export const RoundCheck = ({ checked }: { checked: boolean }) => (
  <svg
    viewBox="0 0 20 20"
    aria-hidden
    focusable="false"
    className="block size-5 flex-none"
  >
    {checked ? (
      <>
        <circle cx="10" cy="10" r="10" className="fill-round-check-bg" />
        <path
          d="M6 10.25l2.75 2.75 5.25-6"
          fill="none"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="stroke-on-accent"
        />
      </>
    ) : (
      <circle
        cx="10"
        cy="10"
        r="9.25"
        fill="none"
        strokeWidth="1.5"
        className="stroke-round-check-ring"
      />
    )}
  </svg>
);
