// shadcn/ui Radio Group (`pnpm dlx shadcn@latest add radio-group`), on our tokens: DESIGN.md §6
// "Radio group". It keeps shadcn's parts and `data-slot` attributes and takes no `className`; the
// lucide circle is swapped for our drawn CheckMark, the round mark of the checkbox, which also
// draws the ring of an item not chosen, so the indicator is the mark itself. An item is the mark
// and its label in one row, the whole row the hit target.
import { RadioGroup as RadioGroupPrimitive } from "radix-ui";
import { useId } from "react";
import type { ReactNode } from "react";

import { CheckMark } from "#/components/check-mark.tsx";
import type { OwnProps } from "#/lib/utils.ts";

export type RadioGroupProps = Omit<
  OwnProps<typeof RadioGroupPrimitive.Root>,
  "value" | "defaultValue"
> & {
  /** Always set: the caller holds the choice. */
  value: string;
};

const RadioGroup = (props: RadioGroupProps) => (
  <RadioGroupPrimitive.Root
    data-slot="radio-group"
    className="flex flex-col gap-2.5"
    {...props}
  />
);

export type RadioGroupItemProps = Omit<
  OwnProps<typeof RadioGroupPrimitive.Item>,
  "children" | "id"
> & {
  /** Whether this item is the one chosen: the mark follows it. */
  checked: boolean;
  /** The item's name, in `type.body` `text.primary`. */
  label: ReactNode;
};

const RadioGroupItem = ({ checked, label, ...props }: RadioGroupItemProps) => {
  const id = useId();
  return (
    <label htmlFor={id} className="flex cursor-pointer items-center gap-2.5">
      <RadioGroupPrimitive.Item
        data-slot="radio-group-item"
        id={id}
        className="rounded-checkbox size-checkbox inline-flex shrink-0 items-center justify-center border-0 bg-transparent p-0 outline-none disabled:cursor-not-allowed disabled:opacity-50"
        {...props}
      >
        <RadioGroupPrimitive.Indicator
          data-slot="radio-group-indicator"
          forceMount
          className="flex"
        >
          <CheckMark checked={checked} />
        </RadioGroupPrimitive.Indicator>
      </RadioGroupPrimitive.Item>
      <span className="type-body text-text-primary">{label}</span>
    </label>
  );
};

export { RadioGroup, RadioGroupItem };
