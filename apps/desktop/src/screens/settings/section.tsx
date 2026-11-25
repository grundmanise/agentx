// One card of the Settings page (DESIGN.md §6 "Settings"): its title, then its rows, a hairline
// between two of them. Cards stack in the order `settings-screen.tsx` lists them; a card that has
// nothing to show renders nothing.
//
// A row (`SettingsRow`) is the setting's name over its line at the left and its control at the
// right; what shows under the row, such as a field or a result, comes after them. A row with no
// name is only what it holds, such as the Editor's radio group. With
// `htmlFor`, the whole row labels that control, so a click anywhere on it toggles a switch.
import { useId } from "react";
import type { ReactNode } from "react";

export interface SettingsCardProps {
  /** The card's title, such as "This machine". */
  title: string;
  children: ReactNode;
}

export const SettingsCard = ({ title, children }: SettingsCardProps) => {
  const id = useId();
  return (
    <section
      aria-labelledby={id}
      className="bg-card-bg border-card-border rounded-card pt-settings-card-top px-settings-card-x pb-settings-card-bottom border"
    >
      <h2 id={id} className="type-settings-label text-text-primary m-0">
        {title}
      </h2>
      <div className="divide-card-border flex flex-col divide-y">
        {children}
      </div>
    </section>
  );
};

export interface SettingsRowProps {
  /** The setting's name, in `type.body` `text.primary`. */
  name?: ReactNode;
  /** The line under the name, in `type.secondary` `text.muted`. */
  line?: ReactNode;
  /** The line's id, for the control's `aria-describedby`. */
  lineId?: string;
  /** The control at the right: a switch, buttons, a value. */
  control?: ReactNode;
  /** The id of the control the row labels. */
  htmlFor?: string;
  /** What shows under the row. */
  children?: ReactNode;
}

export const SettingsRow = ({
  name,
  line,
  lineId,
  control,
  htmlFor,
  children,
}: SettingsRowProps) => {
  if (name === undefined) {
    return <div className="py-settings-row">{children}</div>;
  }
  const head = (
    <>
      <span className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="type-body text-text-primary">{name}</span>
        {line !== undefined && (
          <span id={lineId} className="type-secondary text-text-muted">
            {line}
          </span>
        )}
      </span>
      {control !== undefined && (
        <span className="flex max-w-3/5 flex-none flex-wrap items-center justify-end gap-2">
          {control}
        </span>
      )}
    </>
  );
  return (
    <div className="py-settings-row gap-settings-row flex flex-col">
      {htmlFor === undefined ? (
        <div className="flex items-center justify-between gap-6">{head}</div>
      ) : (
        <label
          htmlFor={htmlFor}
          className="flex cursor-pointer items-center justify-between gap-6"
        >
          {head}
        </label>
      )}
      {children}
    </div>
  );
};
