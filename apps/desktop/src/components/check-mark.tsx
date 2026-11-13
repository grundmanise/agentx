/**
 * The round 16px check mark (DESIGN.md §6 "Checkbox (popover)"): an inset ring in `checkbox.ring`
 * when unchecked, a `checkbox.checked-bg` fill with a `checkbox.check` check when checked, and
 * the same fill with a dash when some of what it stands for is checked. Drawn once, for the
 * popover's native checkbox and the shadcn Checkbox alike.
 */
export const CheckMark = ({
  checked,
}: {
  checked: boolean | "indeterminate";
}) => (
  <svg
    viewBox="0 0 16 16"
    aria-hidden
    focusable="false"
    className="size-checkbox block flex-none"
  >
    {checked === "indeterminate" && (
      <>
        <circle cx="8" cy="8" r="8" className="fill-checkbox-checked-bg" />
        <path
          d="M5 8h6"
          fill="none"
          strokeWidth="1.75"
          strokeLinecap="round"
          className="stroke-checkbox-check"
        />
      </>
    )}
    {checked === true && (
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
    )}
    {checked === false && (
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
