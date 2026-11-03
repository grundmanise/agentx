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

/** The row that opens an item: a chevron, then `children`. Named by `label` alone. */
export const AccordionTrigger = ({
  label,
  open,
  children,
}: {
  /** The row's accessible name: what the item is about. */
  label: string;
  /** Whether the item is open, which turns the chevron down. */
  open: boolean;
  children: ReactNode;
}) => (
  <AccordionPrimitive.Header className="m-0 flex">
    <AccordionPrimitive.Trigger
      aria-label={label}
      data-slot="accordion-trigger"
      className="h-file-row px-file-row hover:bg-file-row-hover focus-visible:bg-file-row-hover flex min-w-0 flex-1 items-center gap-3 text-left outline-none"
    >
      <span className={chevronVariants({ open })}>
        <ChevronIcon size="xs" />
      </span>
      {children}
    </AccordionPrimitive.Trigger>
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
