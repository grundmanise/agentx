import type { ReactNode } from "react";

/**
 * The facts under a detail pane's title (DESIGN.md §6 "Detail facts"): one row that wraps,
 * `detail-facts.gap` apart, as under a plugin's or a machine's name.
 */
export const DetailFacts = ({ children }: { children: ReactNode }) => (
  <dl
    aria-label="Details"
    className="gap-x-detail-facts gap-y-detail-facts-y m-0 flex flex-wrap"
  >
    {children}
  </dl>
);

/** One fact: its label, with its value under it. */
export const DetailFact = ({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) => (
  <div className="flex min-w-0 flex-col gap-1">
    <dt className="type-secondary text-text-hint">{label}</dt>
    <dd className="type-technical-sm text-text-secondary m-0 flex min-h-5 min-w-0 flex-wrap items-center gap-2 break-all">
      {children}
    </dd>
  </div>
);
