// DESIGN.md §6 "Callout": a toned box with an icon tile, a title, a line of text under it and its
// buttons on the right, on a line of their own under the text when they would leave it narrower
// than `--container-banner-text`. The detail pane's banner (src/screens/skills/banner-view.tsx) and the
// notice under AGENTS when an agent sees more than one skill of a name
// (src/screens/skills/sights-view.tsx) are both callouts.
import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

import { AlertIcon } from "#/components/icons/alert.tsx";
import { RefreshIcon } from "#/components/icons/refresh.tsx";

const calloutVariants = cva(
  "rounded-banner py-banner pr-banner-right pl-banner gap-banner flex flex-wrap items-center border",
  {
    variants: {
      tone: {
        attention: "border-tone-attention-border bg-tone-attention-bg",
        danger: "border-tone-danger-border bg-tone-danger-bg",
        neutral: "border-tone-neutral-border bg-tone-neutral-bg",
        primary: "border-tone-primary-border bg-tone-primary-bg",
        yours: "border-tone-yours-border bg-tone-yours-bg",
      },
    },
  }
);

const tileVariants = cva(
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

export type CalloutTone =
  | "attention"
  | "danger"
  | "neutral"
  | "primary"
  | "yours";

export const Callout = ({
  tone,
  icon = "alert",
  title,
  text,
  actions,
  label = title,
}: {
  tone: CalloutTone;
  /** The tile's icon: `update` for an update, `alert` for everything else. */
  icon?: "alert" | "update";
  title: string;
  /** The line under the title: words, or words with a control in them. */
  text?: ReactNode;
  /** The buttons on the right. */
  actions?: ReactNode;
  /** What a screen reader calls the note; the title unless given. */
  label?: string;
}) => (
  <div role="note" aria-label={label} className={calloutVariants({ tone })}>
    <span className={tileVariants({ tone })}>
      {icon === "update" ? <RefreshIcon size="lg" /> : <AlertIcon size="lg" />}
    </span>
    <div className="gap-banner-text min-w-banner-text basis-banner-text flex flex-1 flex-col">
      <span className="type-banner-title text-banner-title">{title}</span>
      {text !== undefined && (
        <span className="type-secondary text-banner-text text-pretty">
          {text}
        </span>
      )}
    </div>
    {actions !== undefined && (
      <div className="ml-auto flex items-center gap-1.5">{actions}</div>
    )}
  </div>
);
