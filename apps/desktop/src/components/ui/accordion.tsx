// shadcn/ui Accordion (`pnpm dlx shadcn@latest add accordion`), on our tokens. It keeps shadcn's
// parts and `data-slot` attributes and takes no `className`: the look comes from the variants.
// `card` is DESIGN.md §6 "File list": one card, rows of `file-row.height` with a hairline between
// them. `list` is the Sources list (DESIGN.md §6 "Sources screen"): each item its own card, which
// the parent places. Both have a chevron that turns down when a row is open. Several rows can be
// open at once, so the root is always `type="multiple"`; its animation classes are dropped
// (DESIGN.md §8).
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Accordion as AccordionPrimitive } from "radix-ui";
import type { ReactNode } from "react";

import { ChevronIcon } from "#/components/icons/chevron.tsx";
import type { OwnProps } from "#/lib/utils.ts";

const accordionVariants = cva("flex flex-col", {
  defaultVariants: { variant: "card" },
  variants: {
    variant: {
      card: "rounded-file-card border-card-border bg-card-bg overflow-hidden border",
      list: "",
    },
  },
});

// A `card` item is a row of the one card, a hairline under it. A `list` item is a card of its own
// (`add-card.*`): `card.bg` with a 1px `card.border`, `add-card.radius`, hover
// `add-card.hover-border`; selected, `add-card.selected-bg` and `add-card.selected-border`.
const itemVariants = cva("", {
  compoundVariants: [
    {
      className:
        "border-card-border bg-card-bg hover:border-add-card-hover-border",
      selected: false,
      variant: "list",
    },
    {
      className: "border-add-card-selected-border bg-add-card-selected-bg",
      selected: true,
      variant: "list",
    },
  ],
  defaultVariants: { selected: false, variant: "card" },
  variants: {
    selected: {
      false: "",
      true: "",
    },
    variant: {
      card: "border-file-row-divider border-b last:border-b-0",
      list: "rounded-add-card overflow-hidden border",
    },
  },
});

// The row holds the heading with the trigger, then the action beside the heading, outside it.
// A `card` row turns `file-row.hover` while hovered or focused; the trigger draws no outline. A
// `list` card shows hover on its border, and the trigger and the action draw the focus outline
// inside the card, which clips anything outside it.
const rowVariants = cva("group relative m-0 flex items-center", {
  defaultVariants: { variant: "card" },
  variants: {
    variant: {
      card: "hover:bg-file-row-hover focus-within:bg-file-row-hover",
      list: "",
    },
  },
});

// A `card` heading holds the trigger and its content. A `list` heading lies under the whole row
// and its trigger fills it, so that a link in the row's content can take its own clicks.
const headingVariants = cva("m-0 flex", {
  defaultVariants: { variant: "card" },
  variants: {
    variant: {
      card: "min-w-0 flex-1",
      list: "absolute inset-0",
    },
  },
});

const triggerVariants = cva("", {
  defaultVariants: { variant: "card" },
  variants: {
    variant: {
      card: "h-file-row px-file-row flex min-w-0 flex-1 items-center gap-3 text-left outline-none",
      list: "rounded-add-card flex-1 focus-visible:-outline-offset-2",
    },
  },
});

// What a `list` row shows, over its trigger: clicks pass through to the trigger, except on a
// control that takes them with `pointer-events-auto`.
const LIST_CONTENT =
  "h-file-row px-file-row pointer-events-none relative flex min-w-0 flex-1 items-center gap-3";

// The row's own control. On a `card` row it sits over the meta text at the row's end and fades in
// while the row is hovered or holds the focus, or while its menu is open, as the meta text fades
// out. On a `list` card it follows the trigger and always shows.
const actionVariants = cva("flex items-center", {
  compoundVariants: [
    { className: "opacity-0", shown: false, variant: "card" },
    { className: "opacity-100", shown: true, variant: "card" },
  ],
  defaultVariants: { shown: false, variant: "card" },
  variants: {
    shown: {
      false: "",
      true: "",
    },
    variant: {
      card: "right-file-row h-file-row absolute top-0 transition-opacity duration-150 group-focus-within:opacity-100 group-hover:opacity-100",
      list: "relative flex-none pr-2.5",
    },
  },
});

// The meta text at the row's end. On a row with a control it gives way to it: it fades out while
// the row is hovered or holds the focus, or while the control's menu is open.
const metaVariants = cva("ml-auto flex flex-none items-center", {
  compoundVariants: [
    {
      className:
        "transition-opacity duration-150 group-focus-within:opacity-0 group-hover:opacity-0",
      gives: true,
      hidden: false,
    },
  ],
  variants: {
    gives: {
      false: "",
      true: "",
    },
    hidden: {
      false: "",
      true: "opacity-0",
    },
  },
});

const chevronVariants = cva(
  "text-file-row-meta flex flex-none transition-transform duration-150",
  {
    variants: {
      open: {
        false: "",
        true: "rotate-90",
      },
    },
  }
);

const Accordion = ({
  label,
  value,
  onValueChange,
  variant,
  children,
}: VariantProps<typeof accordionVariants> & {
  /** The accessible name of the list. */
  label: string;
  /** The values of the open items. */
  value: string[];
  onValueChange: (value: string[]) => void;
  children: ReactNode;
}) => (
  <AccordionPrimitive.Root
    type="multiple"
    value={value}
    onValueChange={onValueChange}
    aria-label={label}
    data-slot="accordion"
    className={accordionVariants({ variant })}
  >
    {children}
  </AccordionPrimitive.Root>
);

const AccordionItem = ({
  variant,
  selected,
  ...props
}: OwnProps<typeof AccordionPrimitive.Item> &
  VariantProps<typeof itemVariants>) => (
  <AccordionPrimitive.Item
    data-slot="accordion-item"
    className={itemVariants({ selected, variant })}
    {...props}
  />
);

/**
 * The row that opens an item: a chevron, then `children`, then `meta` at the right end. Named by
 * `label` alone. `action` is a control of the row's own, beside the trigger rather than inside
 * it: it sits over the meta text at the row's end and fades in while the row is hovered or holds
 * the focus, or while its menu is open, as the meta fades out, so the row's layout never changes;
 * it is in the tab order throughout. In a `list`, the action follows the trigger and always shows,
 * and the trigger lies under the whole row with `children` over it: a link among them, such as one
 * that shows only while the item is open, takes its own clicks with `pointer-events-auto`. Hide
 * the text of `children` that repeats `label` from assistive technology.
 */
const AccordionTrigger = ({
  variant,
  label,
  description,
  open,
  busy = false,
  meta,
  action,
  actionShown = false,
  children,
}: VariantProps<typeof triggerVariants> & {
  /** The row's accessible name: what the item is about. */
  label: string;
  /** The id of the element whose text is the row's accessible description. */
  description?: string;
  /** Whether the item is open, which turns the chevron down. */
  open: boolean;
  /** Whether what the item shows is being read, before it opens. */
  busy?: boolean;
  /** The text at the row's end, such as "6 lines". */
  meta?: ReactNode;
  /** A button over the meta text, such as an icon button. */
  action?: ReactNode;
  /** Keeps the action shown, and the meta hidden, while something of its own is open, such as its menu. */
  actionShown?: boolean;
  children: ReactNode;
}) => {
  const list = variant === "list";
  // In a `list`, the row's description holds the meta text.
  const metaText = meta !== undefined && (
    <span
      aria-hidden={list || undefined}
      className={metaVariants({
        gives: action !== undefined && variant !== "list",
        hidden: action !== undefined && variant !== "list" && actionShown,
      })}
    >
      {meta}
    </span>
  );
  const chevron = (
    <span className={chevronVariants({ open })}>
      <ChevronIcon size="xs" />
    </span>
  );
  return (
    <div data-slot="accordion-row" className={rowVariants({ variant })}>
      <AccordionPrimitive.Header
        data-slot="accordion-header"
        className={headingVariants({ variant })}
      >
        <AccordionPrimitive.Trigger
          aria-label={label}
          aria-describedby={description}
          aria-busy={busy}
          data-slot="accordion-trigger"
          className={triggerVariants({ variant })}
        >
          {!list && (
            <>
              {chevron}
              {children}
              {metaText}
            </>
          )}
        </AccordionPrimitive.Trigger>
      </AccordionPrimitive.Header>
      {list && (
        <div data-slot="accordion-content-row" className={LIST_CONTENT}>
          {chevron}
          {children}
          {metaText}
        </div>
      )}
      {action !== undefined && (
        <span className={actionVariants({ shown: actionShown, variant })}>
          {action}
        </span>
      )}
    </div>
  );
};

const AccordionContent = ({
  ...props
}: OwnProps<typeof AccordionPrimitive.Content>) => (
  <AccordionPrimitive.Content
    data-slot="accordion-content"
    className="overflow-hidden"
    {...props}
  />
);

export { Accordion, AccordionItem, AccordionTrigger, AccordionContent };
