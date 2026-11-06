// shadcn/ui Input (`pnpm dlx shadcn@latest add input`), on our tokens: DESIGN.md §6 "Inputs".
// It keeps shadcn's shape (`data-slot="input"`, every attribute of a native input) and takes no
// `className`: the field's look is its `kind`. A field's icon, label or hint is laid out by the
// element that holds it.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";

import type { OwnProps } from "#/lib/utils.ts";

const inputVariants = cva(
  "border-input-border bg-input-bg text-input-text focus:border-input-border-focus w-full min-w-0 border outline-none focus-visible:outline-none",
  {
    defaultVariants: { kind: "default" },
    variants: {
      kind: {
        // `input.height`, `input.radius`, `input.padding-x`.
        default: "rounded-input h-input px-input type-input",
        // The same, in the mono face: a URL or a command.
        mono: "rounded-input h-input px-input type-input-mono",
        // The Add skills search: a pill of `add-search.height` with room for the icon at its left.
        search:
          "rounded-input-search h-add-search pr-input-search-right pl-input-search-left type-input py-0",
      },
    },
  }
);

export type InputProps = OwnProps<"input"> & VariantProps<typeof inputVariants>;

/** Style comes only from `kind`: Input takes no `className` or `style`. */
const Input = ({ kind, type = "text", ...props }: InputProps) => (
  <input
    data-slot="input"
    type={type}
    spellCheck={false}
    autoComplete="off"
    className={inputVariants({ kind })}
    {...props}
  />
);

export { Input, inputVariants };
