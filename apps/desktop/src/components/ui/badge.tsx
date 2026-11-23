// shadcn/ui Badge (`pnpm dlx shadcn@latest add badge`), on our tokens: DESIGN.md §6 "Pills
// (status)". It keeps shadcn's shape (a `<span>`, or with `asChild` the child element with the
// badge's look, through Slot; `data-slot="badge"`) with our tones in place of shadcn's variants.
// 20px high in list rows, 22 in the detail header. A plugin's version is in mono
// (`type.version-pill`); every other pill is `type.pill`. `BadgeButton` is a Badge on a
// `<button>` (`asChild`), for a pill that opens something or explains itself in a Tooltip: the
// app builds a native button only in `src/components`.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import { Slot } from "radix-ui";

import { cn } from "#/lib/utils.ts";
import type { OwnProps } from "#/lib/utils.ts";

const badgeVariants = cva(
  "rounded-pill inline-flex flex-none items-center gap-1 px-2 whitespace-nowrap",
  {
    compoundVariants: [
      {
        className: "hover:bg-pill-neutral-hover",
        clickable: true,
        tone: "neutral",
      },
      {
        className: "hover:bg-pill-update-hover",
        clickable: true,
        tone: "update",
      },
    ],
    defaultVariants: { clickable: false, face: "sans", size: "row" },
    variants: {
      clickable: {
        false: "",
        true: "",
      },
      face: {
        mono: "type-version-pill",
        sans: "type-pill",
      },
      size: {
        detail: "h-5.5",
        row: "h-5",
      },
      tone: {
        disabled: "bg-pill-disabled-bg text-pill-disabled-text",
        neutral: "bg-pill-neutral-bg text-pill-neutral-text",
        removed: "bg-pill-removed-bg text-pill-removed-text",
        universal: "bg-pill-universal-bg text-pill-universal-text",
        update: "bg-pill-update-bg text-pill-update-text",
        version: "bg-pill-version-bg text-pill-version-text",
      },
    },
  }
);

type BadgeVariants = VariantProps<typeof badgeVariants>;

/** Style comes only from the variants: Badge takes no `className` or `style`. */
export type BadgeProps = OwnProps<"span"> &
  Omit<BadgeVariants, "tone"> & {
    tone: NonNullable<BadgeVariants["tone"]>;
    /**
     * Renders the child element with the badge's look instead of a `<span>`, as `BadgeButton`
     * does with its `<button>`.
     */
    asChild?: boolean;
  };

/** A status pill that only shows: "Not published" on a skill row, a plugin's "Disabled" or its version. */
const Badge = ({
  tone,
  size,
  face,
  clickable,
  asChild = false,
  ...props
}: BadgeProps) => {
  const className = badgeVariants({ clickable, face, size, tone });
  return asChild ? (
    <Slot.Root data-slot="badge" className={className} {...props} />
  ) : (
    <span data-slot="badge" className={className} {...props} />
  );
};

export type BadgeButtonProps = Omit<OwnProps<"button">, "type"> &
  Pick<BadgeProps, "tone" | "size" | "face"> & {
    /**
     * The help cursor, for a pill that does nothing on a click: a button only so that its
     * tooltip opens from the keyboard too, such as the agent picker's Universal pill.
     */
    help?: boolean;
  };

/**
 * A pill that is a button: the fork pill that opens the fork, the sidebar's update pill that
 * opens the Update skills dialog, with the hover fill of its tone; or, with `help`, a pill whose
 * Tooltip explains it, with no hover fill.
 */
const BadgeButton = ({
  tone,
  size,
  face,
  help = false,
  ...props
}: BadgeButtonProps) => (
  <Badge asChild tone={tone} size={size} face={face} clickable={!help}>
    <button type="button" className={cn(help && "cursor-help")} {...props} />
  </Badge>
);

export { Badge, BadgeButton, badgeVariants };
