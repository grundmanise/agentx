// A switch row of a Settings section (DESIGN.md §6 "Settings"): the setting's name over its line,
// the switch on the right. The whole row is the hit target. While the setting's command runs, or
// the settings are read again after it, a spinner turns beside the switch, which takes no click.
import { useId } from "react";

import { Spinner } from "#/components/spinner.tsx";
import { Switch } from "#/components/ui/switch.tsx";
import { cn } from "#/lib/utils.ts";

export const SwitchRow = ({
  name,
  line,
  title,
  checked,
  pending,
  disabled = false,
  onChange,
}: {
  /** The setting's name, which also names the switch. */
  name: string;
  /** The line under the name: what the setting does, or why it cannot change. */
  line: string;
  /** The switch's tooltip: its state and what that means. */
  title: string;
  checked: boolean;
  /** The setting's command runs, or the settings are being read again. */
  pending: boolean;
  /** The setting cannot change now; `line` says why. */
  disabled?: boolean;
  onChange: (on: boolean) => void;
}) => {
  const id = useId();
  return (
    <label
      htmlFor={id}
      className={cn(
        "flex items-start justify-between gap-6",
        !disabled && "cursor-pointer"
      )}
    >
      <span className="flex min-w-0 flex-col gap-1">
        <span className="type-body text-text-primary">{name}</span>
        <span id={`${id}-line`} className="type-secondary text-text-muted">
          {line}
        </span>
      </span>
      <span className="flex flex-none items-center gap-2">
        {pending && (
          <span className="text-text-hint flex">
            <Spinner />
          </span>
        )}
        <Switch
          id={id}
          aria-label={name}
          aria-describedby={`${id}-line`}
          title={title}
          checked={checked}
          disabled={pending || disabled}
          aria-busy={pending ? true : undefined}
          onCheckedChange={onChange}
        />
      </span>
    </label>
  );
};
