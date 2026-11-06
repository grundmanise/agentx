// shadcn/ui Toggle (`pnpm dlx shadcn@latest add toggle`), on our tokens. It keeps shadcn's shape
// (Radix Toggle, `data-slot="toggle"`, `pressed` and `onPressedChange`) and takes no `className`:
// its look is its `kind`, each a control of the Add skills screen that is on or off. Its pressed
// look follows `aria-pressed`, which Radix sets.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Toggle as TogglePrimitive } from "radix-ui";

import type { OwnProps } from "#/lib/utils.ts";

const toggleVariants = cva("inline-flex shrink-0 outline-none", {
  defaultVariants: { kind: "source-pill" },
  variants: {
    kind: {
      // An Add skills card (DESIGN.md §2.2 "Cards and banners", `add-card.*`): `add-card.bg` with a
      // 1px `add-card.border`, `add-card.radius`, `add-card.min-height`; hover `add-card.hover-border`;
      // selected `add-card.selected-bg` and `add-card.selected-border`. A card that cannot be
      // selected sits at 60%.
      card: "rounded-add-card border-add-card-border bg-add-card-bg hover:border-add-card-hover-border aria-pressed:border-add-card-selected-border aria-pressed:bg-add-card-selected-bg min-h-add-card disabled:hover:border-add-card-border w-full items-start gap-3 border p-4 text-left disabled:cursor-default disabled:opacity-60",
      // An agent client logo as an install target: a 28px logo, or the Universal stack of them, at
      // 55% when off, with an `accent.primary` ring when on. The ring takes `pill.radius`, not
      // `logo.radius`: a 50% radius on the stack's 48x28 box is an ellipse that cuts into the
      // logos, while the pill is a circle around one logo and a stadium clear of the stack.
      logo: "rounded-pill ring-offset-overlay-bg aria-pressed:ring-accent-primary items-center justify-center opacity-55 ring-offset-2 aria-pressed:opacity-100 aria-pressed:ring-2",
      // A source pill (DESIGN.md §2.2 "Buttons", `source-pill.*`): `source-pill.bg` with its label
      // in `text.secondary`; the active one `source-pill.active-bg` and `source-pill.active-text`.
      "source-pill":
        "rounded-pill type-button-sm bg-source-pill-bg text-text-secondary hover:text-text-primary aria-pressed:bg-source-pill-active-bg aria-pressed:text-source-pill-active-text aria-pressed:hover:text-source-pill-active-text h-8 items-center gap-2 px-3.5 font-medium whitespace-nowrap",
    },
  },
});

export type ToggleProps = OwnProps<typeof TogglePrimitive.Root> &
  VariantProps<typeof toggleVariants>;

/** Style comes only from `kind`: Toggle takes no `className` or `style`. */
const Toggle = ({ kind, ...props }: ToggleProps) => (
  <TogglePrimitive.Root
    data-slot="toggle"
    className={toggleVariants({ kind })}
    {...props}
  />
);

export { Toggle, toggleVariants };
