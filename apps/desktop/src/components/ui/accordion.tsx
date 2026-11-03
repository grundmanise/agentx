import { cva } from "class-variance-authority";
import { Accordion as AccordionPrimitive } from "radix-ui";
import type { ReactNode } from "react";

import { ChevronIcon } from "#/components/icons/chevron.tsx";

// shadcn/ui's Accordion on Radix, mapped to the tokens of the file list (DESIGN.md §4 "Code,
// diff, timeline, startup" and §6 "Code pane"): the card, rows of `file-row.height` with a
// hairline between them, and a chevron that turns down when a row is open. Several rows can be
// open at once. No `className` or `style`: the look is the one the variants hold.

const accordionVariants = cva("flex flex-col overflow-hidden", {
  defaultVariants: { variant: "card" },
  variants: {
    variant: {
      card: "rounded-file-card border-card-border bg-card-bg border",
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

export const Accordion = ({
  label,
  value,
  onValueChange,
  variant,
  children,
}: {
  /** The accessible name of the list. */
  label: string;
  /** The values of the open items. */
  value: string[];
  onValueChange: (value: string[]) => void;
  variant?: "card";
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

export const AccordionItem = ({
  value,
  children,
}: {
  value: string;
  children: ReactNode;
}) => (
  <AccordionPrimitive.Item
    value={value}
    data-slot="accordion-item"
    className="border-file-row-divider border-b last:border-b-0"
  >
    {children}
  </AccordionPrimitive.Item>
);

/**
 * The row that opens an item: a chevron, then `children`. Named by `label` alone. `action` is a
 * control of the row's own, beside the trigger rather than inside it, shown while the row is
 * hovered or holds the focus; it is in the tab order throughout.
 */
export const AccordionTrigger = ({
  label,
  open,
  action,
  children,
}: {
  /** The row's accessible name: what the item is about. */
  label: string;
  /** Whether the item is open, which turns the chevron down. */
  open: boolean;
  /** A button at the row's right end, such as an icon button. */
  action?: ReactNode;
  children: ReactNode;
}) => (
  <AccordionPrimitive.Header className="group hover:bg-file-row-hover focus-within:bg-file-row-hover m-0 flex items-center">
    <AccordionPrimitive.Trigger
      aria-label={label}
      data-slot="accordion-trigger"
      className="h-file-row px-file-row flex min-w-0 flex-1 items-center gap-3 text-left outline-none"
    >
      <span className={chevronVariants({ open })}>
        <ChevronIcon size="xs" />
      </span>
      {children}
    </AccordionPrimitive.Trigger>
    {action !== undefined && (
      <span className="flex flex-none items-center pr-2 opacity-0 group-focus-within:opacity-100 group-hover:opacity-100">
        {action}
      </span>
    )}
  </AccordionPrimitive.Header>
);

export const AccordionContent = ({ children }: { children: ReactNode }) => (
  <AccordionPrimitive.Content
    data-slot="accordion-content"
    className="overflow-hidden"
  >
    {children}
  </AccordionPrimitive.Content>
);
