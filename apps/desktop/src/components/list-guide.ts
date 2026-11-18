// The nesting pattern of a grouped list (DESIGN.md §6 "Group header (lists)"): what a section
// toggle holds is inset under its caption, with a thin guide line down the left under its
// chevron. Each placed item draws its own piece of the line with these classes, so the line runs
// on through the rows and the spaces between them. Use them with `pl-subgroup-indent` for one
// level and `pl-subgroup-nested` for two.

/** The guide line under a top-level section's chevron. */
export const LIST_GUIDE =
  "before:bg-list-guide before:left-list-guide before:absolute before:inset-y-0 before:w-px";

/** Both guide lines: the top-level section's, and the nested section's under its own chevron. */
export const LIST_GUIDE_NESTED = `${LIST_GUIDE} after:bg-list-guide after:left-list-guide-nested after:absolute after:inset-y-0 after:w-px`;
