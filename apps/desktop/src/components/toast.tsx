import type { ReactNode } from "react";
import { toast } from "sonner";
import type { ExternalToast } from "sonner";

import { problemSentence } from "#/lib/problem-text.ts";

// The toast last shown, which the next one replaces.
let shown: string | number | undefined;

/** Shows a toast in place of the one showing. */
const replace = (show: () => string | number) => {
  const previous = shown;
  shown = show();
  if (previous !== undefined) {
    // Sonner adds a toast on a timer but dismisses one on the next animation frame, which can
    // come first: a toast dismissed before it is added would then stay. A timer set after the
    // one that adds it runs after it.
    setTimeout(() => {
      toast.dismiss(previous);
    }, 0);
  }
  return shown;
};

/** How long a toast stays, in ms: 5s with an action, 2.6s without, and for good when it lasts. */
const durationOf = (withAction: boolean, lasting: boolean) => {
  if (lasting) {
    return Number.POSITIVE_INFINITY;
  }
  return withAction ? 5000 : 2600;
};

/** Takes the toast showing away, for a control that answers a command's outcome itself. */
export const dismissToast = () => {
  if (shown !== undefined) {
    const id = shown;
    shown = undefined;
    // As in replace(): Sonner adds a toast on a timer, so a dismiss sent at once could come first
    // and leave the toast showing.
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
 * What a toast reports (DESIGN.md §6 "Toast"), which sets its icon and its tint: a command that
 * succeeded, one that failed, one that succeeded with warnings, one that runs, or a plain report
 * with no outcome, such as Copied.
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
 * One toast at a time (DESIGN.md §6 "Toast"). A toast with an action stays 5s, one without 2.6s,
 * and a `lasting` one, for a command that runs with its Cancel, until the next toast replaces it.
 * A new toast replaces the one showing; with `inPlace`, it changes the text of the one showing
 * instead, as a running command's toast does when its progress moves. A `progress` toast shows its
 * action as Sonner's cancel button, beside the spinner.
 */
export const notify = (
  message: ReactNode,
  action?: ToastAction,
  {
    kind = "plain",
    lasting = false,
    inPlace = false,
  }: { kind?: ToastKind; lasting?: boolean; inPlace?: boolean } = {}
) => {
  const button = action && { label: action.label, onClick: action.onClick };
  const options: ExternalToast = {
    action: kind === "progress" ? undefined : button,
    cancel: kind === "progress" ? button : undefined,
    duration: durationOf(action !== undefined, lasting),
  };
  if (inPlace && shown !== undefined) {
    return show[kind](message, { ...options, id: shown });
  }
  return replace(() => show[kind](message, options));
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
 * description holds them, then its action when it has one, and its close button, named Dismiss.
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
  return replace(() =>
    show[kind](message, {
      action: action && { label: action.label, onClick: action.onClick },
      closeButton: true,
      description: lines,
      duration,
    })
  );
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
