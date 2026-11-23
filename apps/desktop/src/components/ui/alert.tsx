// shadcn/ui Alert (`pnpm dlx shadcn@latest add alert`), on our tokens: DESIGN.md §6 "Alert". It keeps
// shadcn's parts and their `data-slot` attributes and takes no `className`. Like shadcn's other
// styles it adds AlertAction, the buttons on the right, and it adds AlertTile, the icon tile, and
// AlertContent, the column that holds the title and the text, as shadcn's Item does with ItemMedia
// and ItemContent.
//
// Two departures from shadcn, both on purpose:
// - The box is a wrapping flex row, not shadcn's grid: the buttons go on a line of their own under
//   the text, still on the right, when they would leave it narrower than `--container-banner-text`
//   (`min-w-banner-text basis-banner-text` on AlertContent).
// - A banner's role is `note`, not `alert`: an alert is announced each time it shows, and a banner
//   shows each time a skill opens. A bar is an `<output>` (role `status`), a polite live region.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";
import type { HTMLAttributes } from "react";

import { AlertIcon } from "#/components/icons/alert.tsx";
import { RefreshIcon } from "#/components/icons/refresh.tsx";
import type { OwnProps } from "#/lib/utils.ts";

const alertVariants = cva("flex items-center", {
  defaultVariants: { layout: "banner" },
  variants: {
    layout: {
      // A box, such as the detail pane's banner (DESIGN.md §6 "Alert").
      banner:
        "rounded-banner py-banner pr-banner-right pl-banner gap-banner flex-wrap border",
      // A bar across the top of a panel, a border under it (DESIGN.md §6 "Stale bar"). Its text takes
      // the bar's type and colour, and so does a Spinner in it.
      bar: "h-stale-bar px-stale-bar type-notice text-text-secondary flex-none gap-3 border-b",
    },
    tone: {
      attention: "border-tone-attention-border bg-tone-attention-bg",
      danger: "border-tone-danger-border bg-tone-danger-bg",
      neutral: "border-tone-neutral-border bg-tone-neutral-bg",
      primary: "border-tone-primary-border bg-tone-primary-bg",
      yours: "border-tone-yours-border bg-tone-yours-bg",
    },
  },
});

type AlertTone = NonNullable<VariantProps<typeof alertVariants>["tone"]>;

/**
 * Style comes only from `tone` and `layout`: Alert takes no `className` or `style`. A banner is a
 * note; a bar is an `<output>`, a status region, so a screen reader says when it shows and when its
 * text changes.
 */
const Alert = ({
  tone,
  layout,
  ...props
}: Omit<HTMLAttributes<HTMLElement>, "className" | "style"> &
  Pick<VariantProps<typeof alertVariants>, "layout"> & { tone: AlertTone }) =>
  layout === "bar" ? (
    <output
      data-slot="alert"
      className={alertVariants({ layout, tone })}
      {...props}
    />
  ) : (
    <div
      data-slot="alert"
      role="note"
      className={alertVariants({ layout, tone })}
      {...props}
    />
  );

const alertTileVariants = cva(
  "rounded-card-tile flex size-8 flex-none items-center justify-center",
  {
    variants: {
      tone: {
        attention: "text-tone-attention-fg",
        danger: "text-tone-danger-fg",
        neutral: "text-tone-neutral-fg",
        primary: "text-tone-primary-fg",
        yours: "text-tone-yours-fg",
      },
    },
  }
);

/** The icon tile, in its alert's tone: `update` for an update, `alert` for everything else. */
const AlertTile = ({
  tone,
  icon = "alert",
}: {
  tone: AlertTone;
  icon?: "alert" | "update";
}) => (
  <span data-slot="alert-tile" className={alertTileVariants({ tone })}>
    {icon === "update" ? <RefreshIcon size="lg" /> : <AlertIcon size="lg" />}
  </span>
);

/** The title over the text, as narrow as `--container-banner-text` before the buttons wrap. */
const AlertContent = ({ ...props }: OwnProps<"div">) => (
  <div
    data-slot="alert-content"
    className="gap-banner-text min-w-banner-text basis-banner-text flex flex-1 flex-col"
    {...props}
  />
);

const AlertTitle = ({ ...props }: OwnProps<"span">) => (
  <span
    data-slot="alert-title"
    className="type-banner-title text-banner-title"
    {...props}
  />
);

const alertDescriptionVariants = cva("", {
  defaultVariants: { layout: "banner" },
  variants: {
    layout: {
      banner: "type-secondary text-banner-text text-pretty",
      // One line that takes the bar's width and ends in an ellipsis, in the bar's type and colour.
      bar: "min-w-0 flex-1 truncate",
    },
  },
});

/** The line under the title: words, or words with a control in them; in a bar, its one line. */
const AlertDescription = ({
  layout,
  ...props
}: OwnProps<"span"> & VariantProps<typeof alertDescriptionVariants>) => (
  <span
    data-slot="alert-description"
    className={alertDescriptionVariants({ layout })}
    {...props}
  />
);

/** The buttons on the right, vertically centred. */
const AlertAction = ({ ...props }: OwnProps<"div">) => (
  <div
    data-slot="alert-action"
    className="ml-auto flex items-center gap-1.5"
    {...props}
  />
);

export {
  Alert,
  AlertAction,
  AlertContent,
  AlertDescription,
  AlertTile,
  AlertTitle,
};
