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

/**
 * The round 16px mark of a target that is always on (DESIGN.md §6 "Agent picker"): the checked
 * fill of `CheckMark` with a lock in place of the check, for the Universal agents, which read the
 * library and so get every skill.
 */
export const LockMark = () => (
  <svg
    viewBox="0 0 16 16"
    aria-hidden
    focusable="false"
    className="size-checkbox block flex-none"
  >
    <circle cx="8" cy="8" r="8" className="fill-checkbox-checked-bg" />
    <rect
      x="5"
      y="7.25"
      width="6"
      height="4.5"
      rx="1"
      className="fill-checkbox-check"
    />
    <path
      d="M6.25 7.25V6a1.75 1.75 0 0 1 3.5 0v1.25"
      fill="none"
      strokeWidth="1.4"
      strokeLinecap="round"
      className="stroke-checkbox-check"
    />
  </svg>
);
