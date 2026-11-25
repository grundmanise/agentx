// shadcn/ui Toggle (`pnpm dlx shadcn@latest add toggle`), on our tokens. It keeps shadcn's shape
// (Radix Toggle, `data-slot="toggle"`, `pressed` and `onPressedChange`) and takes no `className`:
// its look is its `kind`, each a control of the Sources screen that is on or off. Its pressed
// look follows `aria-pressed`, which Radix sets.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Toggle as TogglePrimitive } from "radix-ui";

import { trackAttribute } from "#/lib/track.ts";
import type { Tracked } from "#/lib/track.ts";
import type { OwnProps } from "#/lib/utils.ts";

const toggleVariants = cva("inline-flex shrink-0 outline-none", {
  defaultVariants: { kind: "source-pill" },
  variants: {
    kind: {
      // The round check of an agent on the Agents page: the check, whose hit area also covers
      // the whole of the nearest positioned card, under the card's controls, so a click on the
      // card selects it.
      "card-check":
        "rounded-round-check focus-visible:outline-shell-focus-ring size-8 items-center justify-center after:absolute after:inset-0 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-solid",
      // The round check of a skill on the Sources list and of an agent on the Agents page
      // (DESIGN.md §6 "Round check"): a hit area round the check its child draws, which draws its
      // focus outline inside.
      check:
        "rounded-round-check focus-visible:outline-shell-focus-ring size-8 items-center justify-center focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-solid",
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
  VariantProps<typeof toggleVariants> &
  Tracked;

/** Style comes only from `kind`: Toggle takes no `className` or `style`. */
const Toggle = ({ kind, track, ...props }: ToggleProps) => (
  <TogglePrimitive.Root
    {...trackAttribute(track)}
    data-slot="toggle"
    className={toggleVariants({ kind })}
    {...props}
  />
);

export { Toggle, toggleVariants };
