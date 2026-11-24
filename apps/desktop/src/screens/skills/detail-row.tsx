import type { ReactNode } from "react";

/** One row of a skill's DETAILS: its key, `detail.key` wide, then its value. */
export const DetailRow = ({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) => (
  <div className="gap-detail-kv flex">
    <dt className="type-secondary text-text-hint w-detail-key flex-none">
      {label}
    </dt>
    <dd className="type-technical-sm text-text-secondary m-0 min-w-0 flex-1 break-all">
      {children}
    </dd>
  </div>
);
