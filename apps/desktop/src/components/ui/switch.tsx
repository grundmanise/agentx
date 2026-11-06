// shadcn/ui Switch (`pnpm dlx shadcn@latest add switch`), on our tokens: DESIGN.md §6 "Switch".
// It keeps shadcn's parts and `data-slot` attributes and takes no `className`: its size is a
// variant (`switch.size` on the Agents cards, `switch.compact-size` inside a chip), and its colours
// follow `checked`, which the caller sets from what the CLI reported. A switch that waits for its
// command and the inventory that follows is `disabled` with `aria-busy`.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Switch as SwitchPrimitive } from "radix-ui";

import type { OwnProps } from "#/lib/utils.ts";

const switchVariants = cva(
  "rounded-switch p-switch inline-flex shrink-0 items-center border-0 transition-colors duration-200 outline-none disabled:cursor-not-allowed disabled:opacity-50 aria-busy:cursor-wait",
  {
    defaultVariants: { size: "default" },
    variants: {
      on: {
        false: "bg-switch-off-track justify-start",
        true: "bg-switch-on-track justify-end",
      },
      size: {
        compact: "w-switch-compact h-switch-compact",
        default: "w-switch h-switch",
      },
    },
  }
);

const thumbVariants = cva("rounded-switch pointer-events-none block", {
  defaultVariants: { size: "default" },
  variants: {
    on: {
      false: "bg-switch-off-knob",
      true: "bg-switch-on-knob",
    },
    size: {
      compact: "size-switch-compact-knob",
      default: "size-switch-knob",
    },
  },
});

export type SwitchProps = Omit<
  OwnProps<typeof SwitchPrimitive.Root>,
  "checked" | "defaultChecked"
> &
  Pick<VariantProps<typeof switchVariants>, "size"> & {
    /** Always set: a switch shows what the CLI reported, never a guess. */
    checked: boolean;
  };

const Switch = ({ checked, size, ...props }: SwitchProps) => (
  <SwitchPrimitive.Root
    data-slot="switch"
    data-size={size ?? "default"}
    checked={checked}
    className={switchVariants({ on: checked, size })}
    {...props}
  >
    <SwitchPrimitive.Thumb
      data-slot="switch-thumb"
      className={thumbVariants({ on: checked, size })}
    />
  </SwitchPrimitive.Root>
);

export { Switch };
