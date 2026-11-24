// The controls of a list's filter row and its Filter popover (DESIGN.md §2.2 "Filter row",
// §6 "Checkbox (popover)" and "Segmented control"). Each is a closed control: it takes what it
// shows and what to call, never a class. The choices are native radios and checkboxes under
// their labels, covering them unseen so a click lands on the control, so the keyboard and a
// screen reader work as they do on any form.
import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

import { CheckMark } from "#/components/check-mark.tsx";
import { ArrowDownIcon } from "#/components/icons/arrow-down.tsx";
import { CloseIcon } from "#/components/icons/close.tsx";
import { CollapseIcon } from "#/components/icons/collapse.tsx";
import { ExpandIcon } from "#/components/icons/expand.tsx";
import { FilterIcon } from "#/components/icons/filter.tsx";
import { cn } from "#/lib/utils.ts";

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
    className={cn(
      "rounded-button type-button-sm h-filter inline-flex items-center gap-2 px-3 font-medium",
      count > 0
        ? "bg-filter-active-bg text-filter-active-text"
        : "bg-filter-bg text-filter-text hover:text-text-primary"
    )}
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

/** One applied value in a `FilterTagsChip`: what it shows, and the key its removal reports. */
export interface FilterTag {
  key: string;
  label: string;
  /** A mark before the label, such as an agent's logo. */
  icon: ReactNode;
}

/**
 * A chip in the filter row echoing several applied values of one filter. At rest it shows a
 * summary: a few marks and a count. Pointing at it, or tabbing into it, opens a panel over the list
 * in the search field's surface, from the chip's own place, with the summary on top and one tag
 * per value below, inset like the summary's marks, which wrap and scroll past
 * `filter-tags.max-height`. The tags run to the panel's bottom edge, so a tag cut there says the
 * list scrolls; their bottom padding comes only at the end of the scroll. A tag is a button that
 * removes its value; pointing at it shows a cross over the end of its label. The panel has no
 * close control: it goes when the pointer or the focus leaves it.
 */
export const FilterTagsChip = ({
  summary,
  label,
  tags,
  onRemove,
}: {
  /** The marks shown at rest, such as a few logos. */
  summary: ReactNode;
  /** The count, such as "17 agents"; it also names the tags' group. */
  label: string;
  tags: FilterTag[];
  onRemove: (key: string) => void;
}) => (
  <div className="group/tags relative inline-flex min-w-0">
    <span
      aria-hidden
      className="rounded-pill bg-filter-chip-bg type-button-sm text-filter-chip-text h-filter inline-flex min-w-0 items-center gap-2 pr-3 pl-2 group-focus-within/tags:invisible group-hover/tags:invisible"
    >
      {summary}
      <span className="truncate">{label}</span>
    </span>
    <fieldset
      aria-label={label}
      className="rounded-popover border-input-border bg-input-bg shadow-popover w-filter-tags pointer-events-none absolute -top-px -left-px z-30 m-0 flex min-w-0 flex-col overflow-hidden border opacity-0 group-focus-within/tags:pointer-events-auto group-focus-within/tags:opacity-100 group-hover/tags:pointer-events-auto group-hover/tags:opacity-100"
    >
      <span
        aria-hidden
        className="type-button-sm text-filter-chip-text h-filter flex flex-none items-center gap-2 pr-3 pl-2"
      >
        {summary}
        <span className="truncate">{label}</span>
      </span>
      <span className="max-h-filter-tags flex flex-wrap gap-1.5 overflow-y-auto overscroll-contain px-2 pt-1.5 pb-2.5">
        {tags.map((tag) => (
          <button
            key={tag.key}
            type="button"
            aria-label={`Remove ${tag.label}`}
            onClick={() => {
              onRemove(tag.key);
            }}
            className="group/tag rounded-pill bg-filter-chip-bg type-secondary text-text-secondary hover:bg-filter-chip-hover hover:text-text-primary relative inline-flex h-6 max-w-full min-w-0 items-center gap-1.5 overflow-hidden pr-2.5 pl-1"
          >
            {tag.icon}
            <span className="truncate">{tag.label}</span>
            <span className="from-filter-chip-hover via-filter-chip-hover absolute inset-y-0 right-0 flex items-center bg-linear-to-l to-transparent pr-1.5 pl-4 opacity-0 group-hover/tag:opacity-100 group-focus-visible/tag:opacity-100">
              <CloseIcon size="xs" />
            </span>
          </button>
        ))}
      </span>
    </fieldset>
  </div>
);

/** What the sort toggle shows and says for each order. */
const sortLabels = {
  asc: { name: "A to Z", shown: "A-Z" },
  desc: { name: "Z to A", shown: "Z-A" },
} as const;

/**
 * The sort toggle. The list always sorts by name, and the toggle always shows it on the ghost
 * fill: A-Z with the arrow, which turns around for Z-A. Each click flips the order.
 */
export const SortToggle = ({
  order,
  onToggle,
}: {
  order: "asc" | "desc";
  onToggle: () => void;
}) => (
  <button
    type="button"
    aria-label={`Sort: ${sortLabels[order].name}`}
    title={`Sort: ${sortLabels[order].name}`}
    onClick={onToggle}
    className={cn(
      "rounded-button type-button-sm text-filter-sort-text bg-button-ghost-bg hover:bg-button-ghost-hover hover:text-text-primary inline-flex h-7 items-center gap-1 px-2 font-medium",
      order === "desc" && "[&>svg]:rotate-180"
    )}
  >
    <span aria-hidden>{sortLabels[order].shown}</span>
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
  <label
    className={cn(
      "rounded-pill type-secondary has-focus-visible:outline-shell-focus-ring relative inline-flex h-7.5 cursor-pointer items-center gap-2 px-3 has-focus-visible:outline-2 has-focus-visible:outline-offset-2",
      active
        ? "bg-button-secondary-bg text-text-primary"
        : "text-text-muted hover:bg-popover-row-hover hover:text-text-primary"
    )}
  >
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

const trackVariants = cva(
  "rounded-segmented border-segmented-track-border gap-segmented flex border p-0.5",
  {
    defaultVariants: { disabled: false, surface: "overlay" },
    variants: {
      disabled: { false: "", true: "opacity-50" },
      surface: {
        card: "bg-segmented-card-track-bg",
        overlay: "bg-segmented-track-bg",
        page: "bg-segmented-page-track-bg",
      },
    },
  }
);

/**
 * A segmented control of equal segments, one selected: STATE in the popover, the placement on an
 * agent client card, and the view of the Machines screen. While `busy`, its command runs: it is
 * disabled and says so.
 */
export const Segmented = <T extends string>({
  group,
  options,
  value,
  onChange,
  labelledBy,
  title,
  surface,
  disabled = false,
  busy = false,
}: {
  /** The name shared by the radios of the control. */
  group: string;
  options: { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
  /** The id of the text that names the control, when no fieldset's legend does. */
  labelledBy?: string;
  /** What the control decides, on hover. */
  title?: string;
  /** What it sits on: the popover (the default), a card or the page. */
  surface?: "overlay" | "card" | "page";
  disabled?: boolean;
  busy?: boolean;
}) => (
  <div
    role="radiogroup"
    aria-labelledby={labelledBy}
    aria-busy={busy ? true : undefined}
    aria-disabled={disabled || busy ? true : undefined}
    title={title}
    className={trackVariants({ disabled: disabled || busy, surface })}
  >
    {options.map((o) => (
      <label
        key={o.value}
        className={cn(
          "rounded-segmented type-segment has-focus-visible:outline-shell-focus-ring relative inline-flex h-6 flex-1 cursor-pointer items-center justify-center px-2.5 has-focus-visible:outline-2 has-focus-visible:outline-offset-2 has-disabled:cursor-default",
          o.value === value
            ? "bg-segmented-neutral-selected text-text-primary"
            : "text-segmented-text hover:text-text-primary has-disabled:hover:text-segmented-text"
        )}
      >
        <input
          type="radio"
          name={group}
          checked={o.value === value}
          disabled={disabled || busy}
          onChange={() => {
            onChange(o.value);
          }}
          className="absolute inset-0 size-full cursor-pointer opacity-0 disabled:cursor-default"
        />
        {o.label}
      </label>
    ))}
  </div>
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

/**
 * The expand toggle, beside the sort toggle: one icon button on the ghost fill, like the sort
 * toggle, that opens or closes every section of the list at once. It reads Collapse all while any
 * section is open, and Expand all while all are collapsed; its name shows in a tooltip.
 */
export const ExpandToggle = ({
  open,
  disabled = false,
  onToggle,
}: {
  /** Whether any section is open. */
  open: boolean;
  disabled?: boolean;
  onToggle: () => void;
}) => {
  const name = open ? "Collapse all" : "Expand all";
  return (
    <button
      type="button"
      aria-label={name}
      title={name}
      disabled={disabled}
      onClick={onToggle}
      className="rounded-button text-filter-sort-text bg-button-ghost-bg hover:bg-button-ghost-hover hover:text-text-primary inline-flex size-7 items-center justify-center disabled:pointer-events-none disabled:opacity-50"
    >
      {open ? <CollapseIcon size="xs" /> : <ExpandIcon size="xs" />}
    </button>
  );
};
