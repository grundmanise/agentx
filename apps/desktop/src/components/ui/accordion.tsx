// shadcn/ui Accordion (`pnpm dlx shadcn@latest add accordion`), on our tokens: DESIGN.md §6
// "File list". It keeps shadcn's parts and `data-slot` attributes and takes no `className`: the
// card, rows of `file-row.height` with a hairline between them, and a chevron that turns down
// when a row is open, come from the variants. Several rows can be open at once, so the root is
// always `type="multiple"`; its animation classes are dropped (DESIGN.md §8).
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Accordion as AccordionPrimitive } from "radix-ui";
import type { ReactNode } from "react";

import { ChevronIcon } from "#/components/icons/chevron.tsx";
import type { OwnProps } from "#/lib/utils.ts";

const accordionVariants = cva("flex flex-col overflow-hidden", {
  defaultVariants: { variant: "card" },
  variants: {
    variant: {
      card: "rounded-file-card border-card-border bg-card-bg border",
    },
  },
});

// The row's own control, over the meta text at the row's end: it fades in while the row is
// hovered or holds the focus, or while its menu is open, as the meta text fades out.
const actionVariants = cva(
  "right-file-row h-file-row absolute top-0 flex items-center transition-opacity duration-150 group-focus-within:opacity-100 group-hover:opacity-100",
  {
    variants: {
      shown: {
        false: "opacity-0",
        true: "opacity-100",
      },
    },
  }
);

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
  ...props
}: OwnProps<typeof AccordionPrimitive.Item>) => (
  <AccordionPrimitive.Item
    data-slot="accordion-item"
    className="border-file-row-divider border-b last:border-b-0"
    {...props}
  />
);

/**
 * The row that opens an item: a chevron, then `children`, then `meta` at the right end. Named by
 * `label` alone. `action` is a control of the row's own, beside the trigger rather than inside
 * it: it sits over the meta text at the row's end and fades in while the row is hovered or holds
 * the focus, or while its menu is open, as the meta fades out, so the row's layout never changes;
 * it is in the tab order throughout.
 */
const AccordionTrigger = ({
  label,
  open,
  meta,
  action,
  actionShown = false,
  children,
}: {
  /** The row's accessible name: what the item is about. */
  label: string;
  /** Whether the item is open, which turns the chevron down. */
  open: boolean;
  /** The text at the row's end, such as "6 lines". */
  meta?: ReactNode;
  /** A button over the meta text, such as an icon button. */
  action?: ReactNode;
  /** Keeps the action shown, and the meta hidden, while something of its own is open, such as its menu. */
  actionShown?: boolean;
  children: ReactNode;
}) => (
  <AccordionPrimitive.Header
    data-slot="accordion-header"
    className="group hover:bg-file-row-hover focus-within:bg-file-row-hover relative m-0 flex items-center"
  >
    <AccordionPrimitive.Trigger
      aria-label={label}
      data-slot="accordion-trigger"
      className="h-file-row px-file-row flex min-w-0 flex-1 items-center gap-3 text-left outline-none"
    >
      <span className={chevronVariants({ open })}>
        <ChevronIcon size="xs" />
      </span>
      {children}
      {meta !== undefined && (
        <span
          className={metaVariants({
            gives: action !== undefined,
            hidden: action !== undefined && actionShown,
          })}
        >
          {meta}
        </span>
      )}
    </AccordionPrimitive.Trigger>
    {action !== undefined && (
      <span className={actionVariants({ shown: actionShown })}>{action}</span>
    )}
  </AccordionPrimitive.Header>
);

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
