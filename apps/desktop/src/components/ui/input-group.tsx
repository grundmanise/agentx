// shadcn/ui Input Group (`pnpm dlx shadcn@latest add input-group`), on our tokens: DESIGN.md §6
// "Inputs". It keeps shadcn's parts and `data-slot` attributes and takes no `className`: a
// search field is `InputGroup`, its magnifier in an `InputGroupAddon` and the field an
// `InputGroupInput`, our Input, whose `kind` (`list-search` or `search`) draws the field, its
// border and its focus. The addon sits over the field's left padding and lets clicks through to
// the field, so the field's padding tokens place the text; its own `kind` names the same field. Neither part takes shadcn's `role="group"`: a field
// and its icon read as the field alone. The parts our design has no use for (`InputGroupButton`,
// `InputGroupText`, `InputGroupTextarea`, the block and inline-end addons) are left out until a
// design needs them.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";

import { Input } from "#/components/ui/input.tsx";
import type { InputProps } from "#/components/ui/input.tsx";
import type { OwnProps } from "#/lib/utils.ts";

const InputGroup = ({ ...props }: OwnProps<"div">) => (
  <div
    data-slot="input-group"
    className="group/input-group relative flex w-full min-w-0 items-center"
    {...props}
  />
);

const inputGroupAddonVariants = cva(
  "text-input-placeholder pointer-events-none absolute flex items-center select-none",
  {
    defaultVariants: { kind: "list-search" },
    variants: {
      // The Input `kind` of the field it sits in: the list search's `xs` magnifier, or the
      // Sources search's `sm` one, a little further in.
      kind: {
        "list-search": "left-3",
        search: "left-3.5",
      },
    },
  }
);

/** What goes before the text, such as the magnifier: `input.placeholder`, at the field's left. */
const InputGroupAddon = ({
  kind,
  ...props
}: OwnProps<"div"> & VariantProps<typeof inputGroupAddonVariants>) => (
  <div
    data-slot="input-group-addon"
    data-align="inline-start"
    className={inputGroupAddonVariants({ kind })}
    {...props}
  />
);

const InputGroupInput = ({ ...props }: InputProps) => (
  <Input data-slot="input-group-control" {...props} />
);

export { InputGroup, InputGroupAddon, InputGroupInput };
