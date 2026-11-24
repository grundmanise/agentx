// shadcn/ui Toggle Group (`pnpm dlx shadcn@latest add toggle-group`), on our tokens: DESIGN.md §6
// "Segmented control" and §2.2 "Filter row". It keeps shadcn's parts (Radix Toggle Group,
// `data-slot="toggle-group"` and `"toggle-group-item"`, the group's look handed to its items
// through context) and takes no `className`: its look is its `kind`. It is always one of
// (Radix `type="single"`), so its items are radios (`role="radio"`, `aria-checked`) with roving
// focus: Tab reaches the chosen one, the arrow keys move between them, Space or Enter chooses. One
// is always chosen: Radix lets a click on the chosen item clear the choice, and the group ignores
// that. Disabled is 50% opacity on the whole group; `busy` is disabled too, and says so with
// `aria-busy`, for a control whose command runs.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { ToggleGroup as ToggleGroupPrimitive } from "radix-ui";
import { createContext, useContext } from "react";

import { cn } from "#/lib/utils.ts";
import type { OwnProps } from "#/lib/utils.ts";

const toggleGroupVariants = cva("", {
  compoundVariants: [
    {
      className: "bg-segmented-track-bg",
      kind: "segmented",
      surface: "overlay",
    },
    {
      className: "bg-segmented-card-track-bg",
      kind: "segmented",
      surface: "card",
    },
    {
      className: "bg-segmented-page-track-bg",
      kind: "segmented",
      surface: "page",
    },
  ],
  defaultVariants: { surface: "overlay" },
  variants: {
    kind: {
      // The SHOW pills of the Filter popover, wrapping in rows.
      pill: "flex flex-wrap gap-1",
      // A segmented control (DESIGN.md §6 "Segmented control"): the track of equal segments.
      segmented:
        "rounded-segmented border-segmented-track-border gap-segmented flex border p-0.5",
    },
    // What a segmented track sits on: the popover (the default), a card or the page.
    surface: { card: "", overlay: "", page: "" },
  },
});

const toggleGroupItemVariants = cva(
  "focus-visible:outline-shell-focus-ring inline-flex cursor-pointer items-center outline-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-solid",
  {
    variants: {
      kind: {
        // A pill with its label and count: `text.muted` until pointed at, the chosen one on
        // `button.secondary-bg` in `text.primary`.
        pill: "rounded-pill type-secondary data-[state=off]:text-text-muted data-[state=off]:hover:bg-popover-row-hover data-[state=off]:hover:text-text-primary data-[state=on]:bg-button-secondary-bg data-[state=on]:text-text-primary h-7.5 gap-2 px-3",
        // A segment: transparent in `segmented.text`, the chosen one `segmented.neutral-selected`.
        segmented:
          "rounded-segmented type-segment data-[state=off]:text-segmented-text data-[state=off]:enabled:hover:text-text-primary data-[state=on]:bg-segmented-neutral-selected data-[state=on]:text-text-primary h-6 flex-1 justify-center px-2.5 disabled:cursor-default",
      },
    },
  }
);

type Kind = NonNullable<VariantProps<typeof toggleGroupVariants>["kind"]>;

const ToggleGroupContext = createContext<Kind>("segmented");

export type ToggleGroupProps = Omit<
  OwnProps<typeof ToggleGroupPrimitive.Root>,
  "type" | "value" | "defaultValue" | "onValueChange"
> &
  VariantProps<typeof toggleGroupVariants> & {
    kind: Kind;
    /** Always set: the caller holds the choice. */
    value: string;
    /** Called with the item chosen; never with no item. */
    onValueChange: (value: string) => void;
    /** Its command runs: disabled, and `aria-busy`. */
    busy?: boolean;
  };

/** Style comes only from `kind` and `surface`: ToggleGroup takes no `className` or `style`. */
const ToggleGroup = ({
  kind,
  surface,
  value,
  onValueChange,
  disabled = false,
  busy = false,
  children,
  ...props
}: ToggleGroupProps) => (
  <ToggleGroupPrimitive.Root
    data-slot="toggle-group"
    type="single"
    value={value}
    onValueChange={(next) => {
      if (next !== "") {
        onValueChange(next);
      }
    }}
    disabled={disabled || busy}
    aria-disabled={disabled || busy ? true : undefined}
    aria-busy={busy ? true : undefined}
    className={cn(
      toggleGroupVariants({ kind, surface }),
      (disabled || busy) && "opacity-50"
    )}
    {...props}
  >
    <ToggleGroupContext value={kind}>{children}</ToggleGroupContext>
  </ToggleGroupPrimitive.Root>
);

export type ToggleGroupItemProps = OwnProps<typeof ToggleGroupPrimitive.Item>;

/** One choice of the group, drawn as the group's `kind`. */
const ToggleGroupItem = (props: ToggleGroupItemProps) => {
  const kind = useContext(ToggleGroupContext);
  return (
    <ToggleGroupPrimitive.Item
      data-slot="toggle-group-item"
      className={toggleGroupItemVariants({ kind })}
      {...props}
    />
  );
};

export { ToggleGroup, ToggleGroupItem, toggleGroupVariants };
