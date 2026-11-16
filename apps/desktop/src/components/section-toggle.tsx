import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

import { ChevronIcon } from "#/components/icons/chevron.tsx";

const toggleVariants = cva(
  "rounded-row px-subgroup-x py-subgroup-y gap-group focus-visible:outline-shell-focus-ring flex min-w-0 items-center text-left focus-visible:outline-2",
  {
    variants: {
      // With an action, the row that holds the toggle and the action tints instead.
      action: {
        false: "hover:bg-subgroup-hover w-full",
        true: "flex-1",
      },
    },
  }
);

/**
 * The header of a section that collapses, within a list (DESIGN.md §6 "Group header (lists)"):
 * a flat, full-width row with no fill at rest, a chevron that turns down while the section is
 * open, the section's caption and its count. It tints on hover. A click collapses or expands
 * the section, which `aria-expanded` reports. It is named by its caption and count. An action,
 * such as a text button, sits at the row's right end, beside the toggle, so that a click on it
 * toggles nothing.
 */
export const SectionToggle = ({
  label,
  title,
  count,
  open,
  onToggle,
  action,
}: {
  /** The section's caption, uppercase like every caption. */
  label: string;
  /** The caption's tooltip, such as a source's full URL. */
  title?: string;
  /** How many rows the section holds; left out, the header shows no count. */
  count?: number;
  open: boolean;
  onToggle: () => void;
  /** A control at the row's right end, outside the toggle. */
  action?: ReactNode;
}) => {
  const toggle = (
    <button
      type="button"
      aria-expanded={open}
      onClick={onToggle}
      className={toggleVariants({ action: action !== undefined })}
    >
      <span
        className={
          open ? "text-text-caption flex rotate-90" : "text-text-caption flex"
        }
      >
        <ChevronIcon size="xs" />
      </span>
      <span
        title={title}
        className="type-caption text-subgroup-label min-w-0 truncate"
      >
        {label}
      </span>
      {count !== undefined && (
        <span className="type-technical-sm text-subgroup-count">{count}</span>
      )}
    </button>
  );
  if (action === undefined) {
    return toggle;
  }
  return (
    <div className="rounded-row hover:bg-subgroup-hover flex w-full items-center">
      {toggle}
      <span className="pr-subgroup-x flex">{action}</span>
    </div>
  );
};
