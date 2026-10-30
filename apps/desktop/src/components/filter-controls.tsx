// The controls of a list's filter row and its Filter popover (DESIGN.md §2.2 "Filter row",
// §6 "Checkbox (popover)" and "Segmented control"). Each is a closed control: it takes what it
// shows and what to call, never a class. The choices are native radios and checkboxes under
// their labels, covering them unseen so a click lands on the control, so the keyboard and a
// screen reader work as they do on any form.
import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

import { ArrowDownIcon } from "#/components/icons/arrow-down.tsx";
import { CloseIcon } from "#/components/icons/close.tsx";
import { FilterIcon } from "#/components/icons/filter.tsx";

const filterButtonVariants = cva(
  "rounded-button type-button-sm h-filter inline-flex items-center gap-2 px-3 font-medium",
  {
    variants: {
      active: {
        false: "bg-filter-bg text-filter-text hover:text-text-primary",
        true: "bg-filter-active-bg text-filter-active-text",
      },
    },
  }
);

/**
 * The Filter button that opens the popover: `filter.bg` at rest, `filter.active-bg` with a count
 * badge while any filter is applied. As the popover's trigger it takes what the popover gives it
 * (its state attributes, the click and the ref) and passes them to the button.
 */
export const FilterButton = ({
  count,
  ...props
}: {
  /** How many filters are applied; 0 shows the button at rest. */
  count: number;
  onClick?: () => void;
}) => (
  <button
    type="button"
    className={filterButtonVariants({ active: count > 0 })}
    {...props}
  >
    <FilterIcon size="xs" />
    <span>Filter</span>
    {count > 0 && (
      <span
        aria-label={`${count} applied`}
        className="rounded-pill bg-filter-badge-bg type-key-hint text-on-accent size-filter-badge inline-flex items-center justify-center"
      >
        {count}
      </span>
    )}
  </button>
);

/** A chip in the filter row echoing an applied filter, with a cross that clears it. */
export const FilterChip = ({
  children,
  clearLabel,
  onClear,
}: {
  children: ReactNode;
  /** The accessible name of the cross. */
  clearLabel: string;
  onClear: () => void;
}) => (
  <span className="rounded-pill bg-filter-chip-bg type-button-sm text-filter-chip-text h-filter inline-flex min-w-0 items-center gap-2 pr-1 pl-2">
    {children}
    <button
      type="button"
      aria-label={clearLabel}
      onClick={onClear}
      className="rounded-button hover:bg-filter-chip-hover hover:text-text-primary inline-flex size-5.5 items-center justify-center"
    >
      <CloseIcon size="xs" />
    </button>
  </span>
);

const sortVariants = cva(
  "rounded-button type-button-sm text-filter-sort-text hover:bg-button-ghost-hover hover:text-text-primary inline-flex h-7 items-center gap-1 px-2 font-medium",
  {
    variants: {
      descending: {
        false: "",
        true: "[&>svg]:rotate-180",
      },
    },
  }
);

/** The A-Z sort toggle: the arrow turns around when the order is descending. */
export const SortToggle = ({
  descending,
  onToggle,
}: {
  descending: boolean;
  onToggle: () => void;
}) => (
  <button
    type="button"
    aria-label={descending ? "Sort A to Z" : "Sort Z to A"}
    aria-pressed={descending}
    onClick={onToggle}
    className={sortVariants({ descending })}
  >
    <span aria-hidden>{descending ? "Z-A" : "A-Z"}</span>
    <ArrowDownIcon size="xs" />
  </button>
);

/** A group of choices in the popover, under its caption. */
export const ChoiceGroup = ({
  caption,
  children,
}: {
  caption: string;
  children: ReactNode;
}) => (
  <fieldset className="m-0 flex min-w-0 flex-col gap-2 border-0 p-0">
    <legend className="type-caption text-popover-caption mb-2 p-0">
      {caption}
    </legend>
    {children}
  </fieldset>
);

const choiceVariants = cva(
  "rounded-pill type-secondary has-focus-visible:outline-shell-focus-ring relative inline-flex h-7.5 cursor-pointer items-center gap-2 px-3 has-focus-visible:outline-2 has-focus-visible:outline-offset-2",
  {
    variants: {
      active: {
        false:
          "text-text-muted hover:bg-popover-row-hover hover:text-text-primary",
        true: "bg-button-secondary-bg text-text-primary",
      },
    },
  }
);

/** One choice of a one-of group in the popover (SHOW), with its count. */
export const ChoicePill = ({
  group,
  active,
  count,
  children,
  onSelect,
}: {
  /** The name shared by the radios of the group. */
  group: string;
  active: boolean;
  count: number;
  children: string;
  onSelect: () => void;
}) => (
  <label className={choiceVariants({ active })}>
    <input
      type="radio"
      name={group}
      checked={active}
      onChange={onSelect}
      className="absolute inset-0 size-full cursor-pointer opacity-0"
    />
    <span>{children}</span>
    <span
      className={
        active
          ? "type-technical-sm text-text-muted"
          : "type-technical-sm text-text-caption"
      }
    >
      {count}
    </span>
  </label>
);

const segmentVariants = cva(
  "rounded-segmented type-segment has-focus-visible:outline-shell-focus-ring relative inline-flex h-6 flex-1 cursor-pointer items-center justify-center px-2.5 has-focus-visible:outline-2 has-focus-visible:outline-offset-2",
  {
    variants: {
      selected: {
        false: "text-segmented-text hover:text-text-primary",
        true: "bg-segmented-neutral-selected text-text-primary",
      },
    },
  }
);

/** A segmented control of equal segments, one selected (STATE in the popover). */
export const Segmented = <T extends string>({
  group,
  options,
  value,
  onChange,
}: {
  /** The name shared by the radios of the control. */
  group: string;
  options: { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
}) => (
  <div className="rounded-segmented border-segmented-track-border bg-segmented-track-bg gap-segmented flex border p-0.5">
    {options.map((o) => (
      <label
        key={o.value}
        className={segmentVariants({ selected: o.value === value })}
      >
        <input
          type="radio"
          name={group}
          checked={o.value === value}
          onChange={() => {
            onChange(o.value);
          }}
          className="absolute inset-0 size-full cursor-pointer opacity-0"
        />
        {o.label}
      </label>
    ))}
  </div>
);

/** The round popover checkbox, drawn (DESIGN.md §6 "Checkbox (popover)"). */
const CheckMark = ({ checked }: { checked: boolean }) => (
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

/** A row of a many-of group in the popover (AGENTS): checkbox, what the caller shows, a label. */
export const CheckOption = ({
  checked,
  onChange,
  children,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  children: ReactNode;
}) => (
  <label className="rounded-popover-row hover:bg-popover-row-hover has-focus-visible:outline-shell-focus-ring relative flex h-8.5 w-full cursor-pointer items-center gap-2.5 px-2 text-left has-focus-visible:outline-2 has-focus-visible:outline-offset-2">
    <input
      type="checkbox"
      checked={checked}
      onChange={(e) => {
        onChange(e.target.checked);
      }}
      className="absolute inset-0 size-full cursor-pointer opacity-0"
    />
    <CheckMark checked={checked} />
    {children}
  </label>
);
