/**
 * The round 16px check mark (DESIGN.md §6 "Checkbox (popover)"): an inset ring in `checkbox.ring`
 * when unchecked, a `checkbox.checked-bg` fill with a `checkbox.check` check when checked. Drawn
 * once, for the popover's native checkbox and the shadcn Checkbox alike.
 */
export const CheckMark = ({ checked }: { checked: boolean }) => (
  <svg
    viewBox="0 0 16 16"
    aria-hidden
    focusable="false"
    className="size-checkbox block flex-none"
  >
    {checked ? (
      <>
        <circle cx="8" cy="8" r="8" className="fill-checkbox-checked-bg" />
        <path
          d="M4.75 8.25l2.25 2.25 4.25-4.75"
          fill="none"
          strokeWidth="1.75"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="stroke-checkbox-check"
        />
      </>
    ) : (
      <circle
        cx="8"
        cy="8"
        r="7.25"
        fill="none"
        strokeWidth="1.5"
        className="stroke-checkbox-ring"
      />
    )}
  </svg>
);
