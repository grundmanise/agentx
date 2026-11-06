import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

import { Spinner } from "#/components/spinner.tsx";

// DESIGN.md §6 "Pills (status)": 20px high in list rows, 22 in the detail header. A plugin's
// version pill is in mono (`type.version-pill`); every other pill is `type.pill`.
const pillVariants = cva(
  "rounded-pill inline-flex flex-none items-center gap-1 px-2 whitespace-nowrap",
  {
    compoundVariants: [
      {
        className: "hover:bg-pill-neutral-hover",
        clickable: true,
        tone: "neutral",
      },
    ],
    defaultVariants: { face: "sans", size: "row" },
    variants: {
      clickable: {
        false: "",
        true: "aria-busy:cursor-wait aria-busy:opacity-50",
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
        update: "bg-pill-update-bg text-pill-update-text",
        version: "bg-pill-version-bg text-pill-version-text",
      },
    },
  }
);

export type PillTone =
  | "disabled"
  | "neutral"
  | "removed"
  | "update"
  | "version";

export interface PillProps {
  tone: PillTone;
  size?: "row" | "detail";
  /** The face: `mono` for a plugin's version, `sans` (the default) for every other pill. */
  face?: "sans" | "mono";
  /** A 12px glyph before the text. */
  icon?: ReactNode;
  children: string;
}

/**
 * A status pill, display only: "Not published", "Removed from source", "Update waiting", a
 * plugin's "Disabled" or its version.
 */
export const Pill = ({ tone, size, face, icon, children }: PillProps) => (
  <span className={pillVariants({ clickable: false, face, size, tone })}>
    {icon}
    {children}
  </span>
);

/**
 * A pill that opens something or runs a command: the fork pill on the original of a fork made
 * under a new name, or Adopt beside the row of a skill another tool installed. `label` is its
 * accessible name and `title` its tooltip; the text is what shows. `pending` shows a Spinner in
 * place of the icon and makes it inert while its command runs.
 */
export const PillButton = ({
  tone,
  size,
  face,
  icon,
  label,
  title,
  onClick,
  pending = false,
  children,
}: PillProps & {
  label: string;
  title: string;
  onClick: () => void;
  pending?: boolean;
}) => (
  <button
    type="button"
    aria-label={label}
    aria-busy={pending ? true : undefined}
    title={title}
    disabled={pending}
    onClick={onClick}
    className={pillVariants({ clickable: true, face, size, tone })}
  >
    {pending ? <Spinner /> : icon}
    {children}
  </button>
);
