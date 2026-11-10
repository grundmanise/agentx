// shadcn/ui Separator (`pnpm dlx shadcn@latest add separator`, brought in by button-group), on our
// tokens. It keeps shadcn's `data-slot` and orientation and takes no `className`; its colour is a
// variant.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Separator as SeparatorPrimitive } from "radix-ui";

import type { OwnProps } from "#/lib/utils.ts";

const separatorVariants = cva(
  "shrink-0 data-[orientation=horizontal]:h-px data-[orientation=horizontal]:w-full data-[orientation=vertical]:h-full data-[orientation=vertical]:w-px",
  {
    defaultVariants: { tone: "divider" },
    variants: {
      tone: {
        // `button.split-divider`, between the buttons of a button group.
        "button-group":
          "bg-button-split-divider self-stretch data-[orientation=vertical]:h-auto",
        // `shell.divider`, the line between sections.
        divider: "bg-shell-divider",
      },
    },
  }
);

const Separator = ({
  orientation = "horizontal",
  decorative = true,
  tone,
  ...props
}: OwnProps<typeof SeparatorPrimitive.Root> &
  VariantProps<typeof separatorVariants>) => (
  <SeparatorPrimitive.Root
    data-slot="separator"
    decorative={decorative}
    orientation={orientation}
    className={separatorVariants({ tone })}
    {...props}
  />
);

export { Separator };
