// shadcn/ui Button Group (`pnpm dlx shadcn@latest add button-group`), on our tokens: DESIGN.md §6
// "Button group". It keeps shadcn's parts and `data-slot` attributes and takes no `className`:
// buttons side by side with no gap, rounded on the group's outer corners only, and a separator
// between buttons that have no border of their own. The parts our design has no use for
// (`ButtonGroupText`, the select and input rules) are left out until a design needs them.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";

import { Separator } from "#/components/ui/separator.tsx";
import type { OwnProps } from "#/lib/utils.ts";

const buttonGroupVariants = cva(
  "flex w-fit items-stretch [&>*]:focus-visible:relative [&>*]:focus-visible:z-10",
  {
    defaultVariants: { orientation: "horizontal" },
    variants: {
      orientation: {
        horizontal:
          "[&>*:not(:first-child)]:rounded-l-none [&>*:not(:last-child)]:rounded-r-none",
        vertical:
          "flex-col [&>*:not(:first-child)]:rounded-t-none [&>*:not(:last-child)]:rounded-b-none",
      },
    },
  }
);

const ButtonGroup = ({
  orientation,
  ...props
}: OwnProps<"fieldset"> & VariantProps<typeof buttonGroupVariants>) => (
  // A fieldset, whose role is group, as shadcn's `div role="group"`.
  <fieldset
    data-slot="button-group"
    data-orientation={orientation ?? "horizontal"}
    className={buttonGroupVariants({ orientation })}
    {...props}
  />
);

/** The line between two buttons of a group, in `button.split-divider`. */
const ButtonGroupSeparator = ({
  orientation = "vertical",
  ...props
}: Omit<OwnProps<typeof Separator>, "tone">) => (
  <Separator
    data-slot="button-group-separator"
    orientation={orientation}
    tone="button-group"
    {...props}
  />
);

export { ButtonGroup, ButtonGroupSeparator, buttonGroupVariants };
