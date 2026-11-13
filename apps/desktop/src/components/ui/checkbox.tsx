// shadcn/ui Checkbox (`pnpm dlx shadcn@latest add checkbox`), on our tokens: DESIGN.md §6
// "Checkbox (popover)". It keeps shadcn's parts and `data-slot` attributes and takes no
// `className`; the lucide check is swapped for our drawn CheckMark, which also draws the ring
// of the unchecked box, so the indicator is the mark itself. `checked` may be "indeterminate", for
// a box that stands for several of which some are checked: the mark then shows a dash.
import { Checkbox as CheckboxPrimitive } from "radix-ui";

import { CheckMark } from "#/components/check-mark.tsx";
import type { OwnProps } from "#/lib/utils.ts";

export type CheckboxProps = Omit<
  OwnProps<typeof CheckboxPrimitive.Root>,
  "checked" | "defaultChecked"
> & {
  /** Always set: the caller holds the choice. */
  checked: boolean | "indeterminate";
};

const Checkbox = ({ checked, ...props }: CheckboxProps) => (
  <CheckboxPrimitive.Root
    data-slot="checkbox"
    checked={checked}
    className="rounded-checkbox size-checkbox inline-flex shrink-0 items-center justify-center border-0 bg-transparent p-0 outline-none disabled:cursor-not-allowed disabled:opacity-50"
    {...props}
  >
    <CheckboxPrimitive.Indicator
      data-slot="checkbox-indicator"
      forceMount
      className="flex"
    >
      <CheckMark checked={checked} />
    </CheckboxPrimitive.Indicator>
  </CheckboxPrimitive.Root>
);

export { Checkbox };
