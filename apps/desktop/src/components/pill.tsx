import { cva } from "class-variance-authority";

// DESIGN.md §6 "Pills (status)": 20px high in list rows, 22 in the detail header.
const pillVariants = cva(
  "rounded-pill type-pill inline-flex flex-none items-center px-2 whitespace-nowrap",
  {
    defaultVariants: { size: "row" },
    variants: {
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

/** A status pill, display only: "Not published", "Removed from source", "Update waiting". */
export const Pill = ({
  tone,
  size,
  children,
}: {
  tone: PillTone;
  size?: "row" | "detail";
  children: string;
}) => <span className={pillVariants({ size, tone })}>{children}</span>;
