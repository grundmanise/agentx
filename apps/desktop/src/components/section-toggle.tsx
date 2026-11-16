import { ChevronIcon } from "#/components/icons/chevron.tsx";

/**
 * The header of a section that collapses, within a list (DESIGN.md §6 "Group header (lists)"):
 * a flat, full-width row with no fill at rest, a chevron that turns down while the section is
 * open, the section's caption and its count. It tints on hover. A click collapses or expands
 * the section, which `aria-expanded` reports. It is named by its caption and count. A copy of a
 * header that the pointer alone uses, as the one a list pins at its top, leaves the tab order.
 */
export const SectionToggle = ({
  label,
  count,
  open,
  onToggle,
  tabbable = true,
}: {
  /** The section's caption, uppercase like every caption. */
  label: string;
  count: number;
  open: boolean;
  onToggle: () => void;
  /** Whether the Tab key reaches it; false for a copy that the pointer alone uses. */
  tabbable?: boolean;
}) => (
  <button
    type="button"
    tabIndex={tabbable ? undefined : -1}
    aria-expanded={open}
    onClick={onToggle}
    className="rounded-row px-subgroup-x py-subgroup-y gap-group hover:bg-subgroup-hover focus-visible:outline-shell-focus-ring flex w-full items-center text-left focus-visible:outline-2"
  >
    <span
      className={
        open ? "text-text-caption flex rotate-90" : "text-text-caption flex"
      }
    >
      <ChevronIcon size="xs" />
    </span>
    <span className="type-caption text-subgroup-label">{label}</span>
    <span className="type-technical-sm text-subgroup-count">{count}</span>
  </button>
);
