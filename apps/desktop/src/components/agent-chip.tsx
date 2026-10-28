import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

/** Whether an agent client sees the skill: on, off, or on through a placement that was displaced. */
export type AgentChipState = "on" | "off" | "displaced";

const chipVariants = cva(
  "rounded-agent-chip h-agent-chip pr-agent-chip-right pl-agent-chip-left gap-agent-chip inline-flex items-center border py-0",
  {
    variants: {
      on: {
        false: "border-agent-chip-off-border bg-agent-chip-off-bg",
        true: "border-agent-chip-on-border bg-agent-chip-on-bg",
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
 * An agent client toggle chip of the detail pane (DESIGN.md §6): logo, name and a compact switch.
 * This one shows state only and reads as a switch that cannot be changed; the placement controls
 * come in a later change.
 */
export const AgentChip = ({
  logo,
  name,
  state,
}: {
  /** The 28px logo, or stacked logos for Universal. */
  logo: ReactNode;
  name: string;
  state: AgentChipState;
}) => {
  const on = state !== "off";
  return (
    <span
      role="switch"
      aria-checked={on}
      aria-readonly
      aria-label={name}
      title={state === "displaced" ? `${name} · Displaced` : undefined}
      className={chipVariants({ on })}
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
      <span aria-hidden className={switchVariants({ on })}>
        <span className={knobVariants({ on })} />
      </span>
    </span>
  );
};
