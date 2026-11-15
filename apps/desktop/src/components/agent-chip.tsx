import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

import { Spinner } from "#/components/spinner.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";

/**
 * Whether an agent client sees the skill: on, off, on through a placement that was displaced, or
 * on only through another client's placement, as Cursor sees one placed in Claude Code.
 */
export type AgentChipState = "on" | "off" | "displaced" | "inherited";

const chipVariants = cva(
  "rounded-agent-chip h-agent-chip pr-agent-chip-right pl-agent-chip-left gap-agent-chip inline-flex items-center border py-0 text-left",
  {
    compoundVariants: [
      {
        class: "hover:border-agent-chip-hover-border",
        inert: false,
        locked: false,
      },
    ],
    variants: {
      inert: {
        false: "",
        true: "cursor-default",
      },
      locked: {
        false: "",
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
      inert: {
        false: "",
        true: "opacity-50",
      },
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
 * which is the caller's to answer. An inert chip, displaced or inherited, keeps its on look with a
 * disabled switch: a click changes nothing, and the Tooltip says why. It stays focusable, so the
 * keyboard reaches the Tooltip too.
 */
export const AgentChip = ({
  logo,
  name,
  state,
  title,
  inert,
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
  /** Why a click changes nothing: the chip's Tooltip, with its switch disabled. */
  inert?: string;
  locked?: boolean;
  pending?: boolean;
  onClick: () => void;
}) => {
  const on = state !== "off";
  const disabled = inert !== undefined && !locked;
  const chip = (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={state === "displaced" ? `${name}, displaced` : name}
      aria-busy={pending ? true : undefined}
      aria-disabled={disabled ? true : undefined}
      title={disabled ? undefined : title}
      onClick={disabled ? undefined : onClick}
      className={chipVariants({ inert: disabled, locked, on, pending })}
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
        <span aria-hidden className={switchVariants({ inert: disabled, on })}>
          <span className={knobVariants({ on })} />
        </span>
      )}
    </button>
  );
  if (!disabled) {
    return chip;
  }
  return (
    <Tooltip>
      <TooltipTrigger asChild>{chip}</TooltipTrigger>
      <TooltipContent>{inert}</TooltipContent>
    </Tooltip>
  );
};
