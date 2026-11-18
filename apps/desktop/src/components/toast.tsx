import type { ReactNode } from "react";
import { toast } from "sonner";
import type { ExternalToast } from "sonner";

import { Button } from "#/components/ui/button.tsx";
import { problemSentence } from "#/lib/problem-text.ts";

// The toast last shown, which `dismissToast` takes away.
let last: string | number | undefined;

// The toast of a command that runs, with its Cancel: the next toast takes its place.
let running: string | number | undefined;

// The id the next toast takes, so its action can dismiss it.
let count = 0;
const nextId = () => {
  count += 1;
  return `toast-${count}`;
};

/**
 * The id a new toast takes: a running command's toast, which it then replaces in place, or a new
 * one, which stacks over the toasts showing.
 */
const idFor = (lasting: boolean) => {
  const id = running ?? nextId();
  running = lasting ? id : undefined;
  last = id;
  return id;
};

/** How long a toast stays, in ms: 5s with an action, 2.6s without, and for good when it lasts. */
const durationOf = (withAction: boolean, lasting: boolean) => {
  if (lasting) {
    return Number.POSITIVE_INFINITY;
  }
  return withAction ? 5000 : 2600;
};

/** Forgets a toast that goes, so that no new toast takes its id. */
const forget = (id: string | number) => {
  if (last === id) {
    last = undefined;
  }
  if (running === id) {
    running = undefined;
  }
};

/** Takes the toast last shown away, for a control that answers a command's outcome itself. */
export const dismissToast = () => {
  if (last !== undefined) {
    const id = last;
    forget(id);
    // Sonner adds a toast on a timer but dismisses one on the next animation frame, which can
    // come first: a toast dismissed before it is added would then stay. A timer set after the
    // one that adds it runs after it.
    setTimeout(() => {
      toast.dismiss(id);
    }, 0);
  }
};

/** An action inside a toast, such as Undo. */
export interface ToastAction {
  label: string;
  onClick: () => void;
}

/**
 * A toast's action, such as Undo, Retry or Cancel, as the app's small ghost button (DESIGN.md §6
 * "Buttons") in place of Sonner's: Sonner shows an element given as its action as it is, at the end
 * of the toast's row. A click takes the toast away and runs the action, whose own toast, such as
 * Stopped, then stacks as a new one.
 */
const actionButton = (id: string | number, action: ToastAction | undefined) =>
  action === undefined ? undefined : (
    <span className="ml-auto flex-none">
      <Button
        variant="ghost"
        size="sm"
        onClick={() => {
          forget(id);
          toast.dismiss(id);
          action.onClick();
        }}
      >
        {action.label}
      </Button>
    </span>
  );

/**
 * What a toast reports (DESIGN.md §6 "Toast"), which sets its icon and its tint: a command that
 * succeeded, one that failed, one that succeeded with warnings, one that runs, or a plain report
 * with no outcome, such as Stopped.
 */
export type ToastKind = "success" | "error" | "warning" | "progress" | "plain";

const show = {
  error: toast.error,
  plain: toast,
  progress: toast.loading,
  success: toast.success,
  warning: toast.warning,
} satisfies Record<
  ToastKind,
  (message: ReactNode, data?: ExternalToast) => string | number
>;

/**
 * Shows a toast (DESIGN.md §6 "Toast"): it stacks over the toasts showing, up to 3. A toast with an
 * action stays 5s, one without 2.6s, and a `lasting` one, for a command that runs with its Cancel,
 * until the next toast takes its place, as the command's progress and then its outcome do.
 */
export const notify = (
  message: ReactNode,
  action?: ToastAction,
  {
    kind = "plain",
    lasting = false,
  }: { kind?: ToastKind; lasting?: boolean } = {}
) => {
  const id = idFor(lasting);
  return show[kind](message, {
    action: actionButton(id, action),
    duration: durationOf(action !== undefined, lasting),
    id,
  });
};

/** Reports that a command succeeded: `notify` with the success icon and tint. */
export const notifySuccess = (message: ReactNode, action?: ToastAction) =>
  notify(message, action, { kind: "success" });

/** Reports that something the app tried failed, in its own words: `notify` with the error icon and tint. */
export const notifyFailure = (message: ReactNode, action?: ToastAction) =>
  notify(message, action, { kind: "error" });

interface DetailToast {
  kind: "error" | "warning";
  message: string;
  /** More lines about what happened, under the message, in `toast.description`. */
  details?: string[];
  /** Under the message and the details, in `tone.attention.fg`. */
  warnings?: string[];
  action?: ToastAction;
  duration: number;
}

/**
 * A toast with lines under its message (DESIGN.md §6 "Attention toast" and "Error toast"): Sonner's
 * description holds them, then its action when it has one, and Sonner's close button, named Dismiss.
 */
const notifyDetail = ({
  kind,
  message,
  details = [],
  warnings = [],
  action,
  duration,
}: DetailToast) => {
  const lines =
    details.length + warnings.length === 0 ? undefined : (
      <span className="flex flex-col gap-1 wrap-break-word">
        {details.map((detail) => (
          <span key={detail}>{detail}</span>
        ))}
        {warnings.map((warning) => (
          <span key={warning} className="text-tone-attention-fg">
            {warning}
          </span>
        ))}
      </span>
    );
  const id = idFor(false);
  return show[kind](message, {
    action: actionButton(id, action),
    closeButton: true,
    description: lines,
    duration,
    id,
  });
};

export interface AttentionToast {
  message: string;
  /** The command's warnings, under the text. */
  warnings: string[];
  action?: ToastAction;
}

/**
 * A command that succeeded with warnings (DESIGN.md §6 "Attention toast"): its success text,
 * then each warning the CLI logged, and its action when it has one. It goes after 5s.
 */
export const notifyAttention = ({
  message,
  warnings,
  action,
}: AttentionToast) =>
  notifyDetail({ action, duration: 5000, kind: "warning", message, warnings });

export interface ErrorToast {
  message: string;
  /** The CLI's fix, after the message: the toast then stays until dismissed. */
  hint?: string;
  /** More lines about what happened, under the message, in `toast.description`. */
  details?: string[];
  /** The command's warnings, under the error. */
  warnings?: string[];
  /** A way out, such as Add source, beside the message. */
  action?: ToastAction;
  /** Names that keep their case when one starts the message or the hint, such as the skills the app holds. */
  keep?: readonly string[];
}

/**
 * A command's error (DESIGN.md §6 "Error toast"): the CLI's message and its hint as one
 * sentence-cased paragraph, its details, then the command's warnings, and its action when it has one. It stays until dismissed
 * when it has a hint, and 5s when it has none.
 */
export const notifyError = ({
  message,
  hint,
  details = [],
  warnings = [],
  action,
  keep,
}: ErrorToast) =>
  notifyDetail({
    action,
    details,
    duration: hint === undefined ? 5000 : Number.POSITIVE_INFINITY,
    kind: "error",
    message: problemSentence(
      hint === undefined ? { message } : { hint, message },
      { keep }
    ),
    warnings,
  });
