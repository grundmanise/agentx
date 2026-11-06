import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

import { Spinner } from "#/components/spinner.tsx";

/** Whether an agent client sees the skill: on, off, or on through a placement that was displaced. */
export type AgentChipState = "on" | "off" | "displaced";

const chipVariants = cva(
  "rounded-agent-chip h-agent-chip pr-agent-chip-right pl-agent-chip-left gap-agent-chip inline-flex items-center border py-0 text-left",
  {
    variants: {
      locked: {
        false: "hover:border-agent-chip-hover-border",
        true: "cursor-not-allowed opacity-45",
      },
      on: {
        false: "border-agent-chip-off-border bg-agent-chip-off-bg",
        true: "border-agent-chip-on-border bg-agent-chip-on-bg",
      },
      pending: {
        false: "",
        true: "cursor-wait",
      },
    },
  }
);

const switchVariants = cva(
  "rounded-switch w-switch-compact h-switch-compact p-switch flex flex-none items-center",
  {
    variants: {
      on: {
        false: "bg-switch-off-track justify-start",
        true: "bg-switch-on-track justify-end",
      },
    },
  }
);

const knobVariants = cva("rounded-switch size-switch-compact-knob block", {
  variants: {
    on: {
      false: "bg-switch-off-knob",
      true: "bg-switch-on-knob",
    },
  },
});

/**
 * An agent client toggle chip of the detail pane (DESIGN.md §6): logo, name and a compact switch,
 * one button. It shows the state the inventory reports, and nothing else: while its command runs
 * and until the next inventory arrives it is pending, with a spinner in place of the switch. A
 * locked chip is dimmed and its `title` says why; it stays a button, since a click toasts the reason,
 * which is the caller's to answer.
 */
export const AgentChip = ({
  logo,
  name,
  state,
  title,
  locked = false,
  pending = false,
  onClick,
}: {
  /** The 28px logo, or stacked logos for Universal. */
  logo: ReactNode;
  name: string;
  state: AgentChipState;
  /** What a click does, or why it does nothing. */
  title: string;
  locked?: boolean;
  pending?: boolean;
  onClick: () => void;
}) => {
  const on = state !== "off";
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={state === "displaced" ? `${name}, displaced` : name}
      aria-busy={pending ? true : undefined}
      title={title}
      onClick={onClick}
      className={chipVariants({ locked, on, pending })}
    >
      {logo}
      <span className="type-agent-chip text-agent-chip-text flex min-w-0 flex-col">
        <span className="truncate">{name}</span>
        {state === "displaced" && (
          <span className="type-technical-sm text-tone-attention-fg">
            displaced
          </span>
        )}
      </span>
      {pending ? (
        <span
          aria-hidden
          className="w-switch-compact text-text-hint flex flex-none justify-center"
        >
          <Spinner />
        </span>
      ) : (
        <span aria-hidden className={switchVariants({ on })}>
          <span className={knobVariants({ on })} />
        </span>
      )}
    </button>
  );
};
