// The controls of a list's filter row and its Filter popover (DESIGN.md §2.2 "Filter row",
// §6 "Checkbox (popover)" and "Segmented control"), composed of the ui primitives: the Filter
// button, the chips' crosses and the sort and expand toggles are `Button`s, the Filter button's
// count a `Badge`, the SHOW pills and the segmented controls a `ToggleGroup`, and an AGENTS row a
// `Checkbox` under its label. The tags of `FilterTagsChip` stay native buttons: a pill with a
// cross that fades in over its label, which no Button variant draws (Button is `button.radius`).
// Each is a closed control: it takes what it shows and what to call, never a class.
import { useId } from "react";
import type { ReactNode } from "react";

import { ArrowDownIcon } from "#/components/icons/arrow-down.tsx";
import { CloseIcon } from "#/components/icons/close.tsx";
import { CollapseIcon } from "#/components/icons/collapse.tsx";
import { ExpandIcon } from "#/components/icons/expand.tsx";
import { FilterIcon } from "#/components/icons/filter.tsx";
import { Badge } from "#/components/ui/badge.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Checkbox } from "#/components/ui/checkbox.tsx";
import { ToggleGroup, ToggleGroupItem } from "#/components/ui/toggle-group.tsx";
import { trackAttribute } from "#/lib/track.ts";
import type { Tracked } from "#/lib/track.ts";
import { cn } from "#/lib/utils.ts";

/**
 * The Filter button that opens the popover: `filter.bg` at rest, `filter.active-bg` with a count
 * badge while any filter is applied. As the popover's trigger it takes what the popover gives it
 * (its state attributes, the click and the ref) and passes them to the button.
 */
export const FilterButton = ({
  count,
  ...props
}: Tracked & {
  /** How many filters are applied; 0 shows the button at rest. */
  count: number;
  onClick?: () => void;
}) => (
  <Button
    variant={count > 0 ? "filter-active" : "filter"}
    size="filter"
    {...props}
  >
    <FilterIcon size="xs" />
    <span>Filter</span>
    {count > 0 && (
      <Badge
        tone="filter-count"
        size="filter-count"
        aria-label={`${count} applied`}
      >
        {count}
      </Badge>
    )}
  </Button>
);

/** A chip in the filter row echoing an applied filter, with a cross that clears it. */
export const FilterChip = ({
  children,
  clearLabel,
  onClear,
  track,
}: Tracked & {
  children: ReactNode;
  /** The accessible name of the cross. */
  clearLabel: string;
  onClear: () => void;
}) => (
  <span className="rounded-pill bg-filter-chip-bg type-button-sm text-filter-chip-text h-filter inline-flex min-w-0 items-center gap-2 pr-1 pl-2">
    {children}
    <Button
      variant="filter-chip"
      size="chip"
      aria-label={clearLabel}
      track={track}
      onClick={onClear}
    >
      <CloseIcon size="xs" />
    </Button>
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
 * close control: it goes when the pointer or the focus leaves it. A click on any tag is sent under
 * `track`.
 */
export const FilterTagsChip = ({
  summary,
  label,
  tags,
  onRemove,
  track,
}: Tracked & {
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
            {...trackAttribute(track)}
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
  track,
}: Tracked & {
  order: "asc" | "desc";
  onToggle: () => void;
}) => (
  <Button
    variant="filter-toggle"
    size="filter-toggle"
    aria-label={`Sort: ${sortLabels[order].name}`}
    title={`Sort: ${sortLabels[order].name}`}
    track={track}
    onClick={onToggle}
  >
    <span
      className={cn(
        "inline-flex items-center gap-1",
        order === "desc" && "[&>svg]:rotate-180"
      )}
    >
      <span aria-hidden>{sortLabels[order].shown}</span>
      <ArrowDownIcon size="xs" />
    </span>
  </Button>
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

/**
 * The choices of a one-of group in the popover (SHOW): a pill per choice, with its count. The
 * fieldset of its `ChoiceGroup` names the radios, so the pills' own group adds no role. A click on
 * any pill is sent under `track`.
 */
export const ChoicePills = <T extends string>({
  options,
  value,
  onChange,
  track,
}: Tracked & {
  options: { value: T; label: string; count: number }[];
  value: T;
  onChange: (value: T) => void;
}) => (
  <ToggleGroup
    kind="pill"
    role="none"
    value={value}
    onValueChange={(next) => {
      const option = options.find((o) => o.value === next);
      if (option !== undefined) {
        onChange(option.value);
      }
    }}
  >
    {options.map((o) => (
      <ToggleGroupItem key={o.value} value={o.value} track={track}>
        <span>{o.label}</span>
        <span
          className={cn(
            "type-technical-sm",
            o.value === value ? "text-text-muted" : "text-text-caption"
          )}
        >
          {o.count}
        </span>
      </ToggleGroupItem>
    ))}
  </ToggleGroup>
);

/**
 * A segmented control of equal segments, one selected: STATE in the popover, and the placement
 * on an agent client card. While `busy`, its command runs: it is disabled and says so. A click on
 * any segment is sent under `track`.
 */
export const Segmented = <T extends string>({
  options,
  value,
  onChange,
  labelledBy,
  title,
  surface,
  disabled = false,
  busy = false,
  track,
}: Tracked & {
  options: { value: T; label: string }[];
  value: T;
  onChange: (value: T) => void;
  /** The id of the text that names the control, when no fieldset's legend does. */
  labelledBy?: string;
  /** What the control decides, on hover. */
  title?: string;
  /** What it sits on: the popover (the default) or a card. */
  surface?: "overlay" | "card";
  disabled?: boolean;
  busy?: boolean;
}) => (
  <ToggleGroup
    kind="segmented"
    surface={surface}
    value={value}
    onValueChange={(next) => {
      const option = options.find((o) => o.value === next);
      if (option !== undefined) {
        onChange(option.value);
      }
    }}
    aria-labelledby={labelledBy}
    title={title}
    disabled={disabled}
    busy={busy}
  >
    {options.map((o) => (
      <ToggleGroupItem key={o.value} value={o.value} track={track}>
        {o.label}
      </ToggleGroupItem>
    ))}
  </ToggleGroup>
);

/**
 * A row of a many-of group in the popover (AGENTS): checkbox, what the caller shows, a label. The
 * whole row is the label of its checkbox, so a click anywhere on it toggles the box.
 */
export const CheckOption = ({
  checked,
  onChange,
  children,
  track,
}: Tracked & {
  checked: boolean;
  onChange: (checked: boolean) => void;
  children: ReactNode;
}) => {
  const id = useId();
  return (
    <label
      htmlFor={id}
      className="rounded-popover-row hover:bg-popover-row-hover has-focus-visible:outline-shell-focus-ring flex h-8.5 w-full cursor-pointer items-center gap-2.5 px-2 text-left has-focus-visible:outline-2 has-focus-visible:outline-offset-2"
    >
      <Checkbox
        id={id}
        checked={checked}
        track={track}
        onCheckedChange={(next) => {
          onChange(next === true);
        }}
      />
      {children}
    </label>
  );
};

/**
 * The expand toggle, beside the sort toggle: one icon button on the ghost fill, like the sort
 * toggle, that opens or closes every section of the list at once. It reads Collapse all while any
 * section is open, and Expand all while all are collapsed; its name shows in a tooltip.
 */
export const ExpandToggle = ({
  open,
  disabled = false,
  onToggle,
  track,
}: Tracked & {
  /** Whether any section is open. */
  open: boolean;
  disabled?: boolean;
  onToggle: () => void;
}) => {
  const name = open ? "Collapse all" : "Expand all";
  return (
    <Button
      variant="filter-toggle"
      size="filter-icon"
      aria-label={name}
      title={name}
      disabled={disabled}
      track={track}
      onClick={onToggle}
    >
      {open ? <CollapseIcon size="xs" /> : <ExpandIcon size="xs" />}
    </Button>
  );
};
