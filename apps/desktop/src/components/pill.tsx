import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

import { Spinner } from "#/components/spinner.tsx";

// DESIGN.md §6 "Pills (status)": 20px high in list rows, 22 in the detail header.
const pillVariants = cva(
  "rounded-pill type-pill inline-flex flex-none items-center gap-1 px-2 whitespace-nowrap",
  {
    compoundVariants: [
      {
        className: "hover:bg-pill-neutral-hover",
        clickable: true,
        tone: "neutral",
      },
    ],
    defaultVariants: { size: "row" },
    variants: {
      clickable: {
        false: "",
        true: "aria-busy:cursor-wait aria-busy:opacity-50",
      },
      size: {
        detail: "h-5.5",
        row: "h-5",
      },
      tone: {
        neutral: "bg-pill-neutral-bg text-pill-neutral-text",
        removed: "bg-pill-removed-bg text-pill-removed-text",
        update: "bg-pill-update-bg text-pill-update-text",
      },
    },
  }
);

export type PillTone = "neutral" | "removed" | "update";

export interface PillProps {
  tone: PillTone;
  size?: "row" | "detail";
  /** A 12px glyph before the text. */
  icon?: ReactNode;
  children: string;
}

/** A status pill, display only: "Not published", "Removed from source", "Update waiting". */
export const Pill = ({ tone, size, icon, children }: PillProps) => (
  <span className={pillVariants({ clickable: false, size, tone })}>
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
    className={pillVariants({ clickable: true, size, tone })}
  >
    {pending ? <Spinner /> : icon}
    {children}
  </button>
);
