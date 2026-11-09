// One section of the Settings page (DESIGN.md §6 "Settings"): the label column with the section's
// label and the line under it, then its content. Sections stack in the order
// `settings-screen.tsx` lists them, with a hairline between two of them; a section that has
// nothing to show renders nothing, and no hairline is left for it.
import { useId } from "react";
import type { ReactNode } from "react";

export interface SettingsSectionProps {
  /** The section label, such as "This machine". */
  title: string;
  /** The line under the label, in `text.muted`; absent for a section that needs none. */
  description?: string;
  children: ReactNode;
}

export const SettingsSection = ({
  title,
  description,
  children,
}: SettingsSectionProps) => {
  const id = useId();
  return (
    <section
      aria-labelledby={id}
      className="grid-cols-settings gap-x-settings-columns py-settings-section border-shell-divider grid border-t first:border-t-0"
    >
      <div className="flex min-w-0 flex-col gap-1">
        <h2 id={id} className="type-settings-label text-text-primary m-0">
          {title}
        </h2>
        {description !== undefined && (
          <p className="type-secondary text-text-muted m-0">{description}</p>
        )}
      </div>
      <div className="gap-settings-section flex min-w-0 flex-col">
        {children}
      </div>
    </section>
  );
};

/** A failure under a field: the CLI's message, and its hint under it. */
export const FieldProblem = ({
  message,
  hint,
}: {
  message: string;
  hint?: string;
}) => (
  <div role="alert" className="flex flex-col gap-1">
    <p className="type-notice text-tone-danger-fg m-0">{message}</p>
    {hint !== undefined && (
      <p className="type-notice text-text-muted m-0">{hint}</p>
    )}
  </div>
);
