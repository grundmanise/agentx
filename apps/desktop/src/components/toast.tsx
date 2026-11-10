import { toast, Toaster as Sonner } from "sonner";
import type { ExternalToast } from "sonner";

import { problemSentence } from "#/lib/problem-text.ts";
import { cn } from "#/lib/utils.ts";

// The toast last shown, which the next one replaces.
let shown: string | number | undefined;

// Wrapped in cn() so the design-system lint reads these strings as classes.
const classNames = {
  actionButton: cn(
    "rounded-button type-toast-action text-button-toast-action-text hover:bg-button-ghost-hover h-7.5 px-3.5"
  ),
  detail: cn(
    "rounded-toast-detail border-overlay-border bg-overlay-bg shadow-popover w-toast-detail py-toast-detail pl-toast flex border pr-1.5"
  ),
  detailContent: cn("min-w-0 flex-1"),
  title: cn("pr-3"),
  // As wide as its text, up to `toast.max-width`, then the text wraps; centred on the window,
  // which Sonner's narrower toaster box does not do for a wider toast. The action keeps its
  // width beside the text, centred on the pill like a one-line toast's.
  toast: cn(
    "rounded-toast border-overlay-border bg-overlay-bg type-toast text-toast-text shadow-popover min-h-toast max-w-toast py-toast-y pl-toast left-1/2 flex w-max -translate-x-1/2 items-center gap-3 border pr-1.5 [&_[data-button]]:flex-none"
  ),
};

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
 * One toast at a time (DESIGN.md §6 "Toast"). A toast with an action stays 5s, one without 2.6s,
 * and a `lasting` one, for a command that runs with its Cancel, until the next toast replaces it.
 * A new toast replaces the one showing; with `inPlace`, it changes the text of the one showing
 * instead, as a running command's toast does when its progress moves.
 */
export const notify = (
  message: string,
  action?: ToastAction,
  {
    lasting = false,
    inPlace = false,
  }: { lasting?: boolean; inPlace?: boolean } = {}
) => {
  const options: ExternalToast = {
    action: action && { label: action.label, onClick: action.onClick },
    classNames: { title: classNames.title, toast: classNames.toast },
    duration: durationOf(action !== undefined, lasting),
  };
  if (inPlace && shown !== undefined) {
    return toast(message, { ...options, id: shown });
  }
  return replace(() => toast(message, options));
};

interface DetailToast {
  message: string;
  /** More lines about what happened, under the message, in `type.notice` `text.muted`. */
  details?: string[];
  /** Under the message and the details, in `tone.attention.fg`. */
  warnings?: string[];
  action?: ToastAction;
  duration: number;
}

/**
 * The wide toast with lines under its message (DESIGN.md §6 "Attention toast" and "Error
 * toast"), its action when it has one, then a ghost Dismiss.
 */
const notifyDetail = ({
  message,
  details = [],
  warnings = [],
  action,
  duration,
}: DetailToast) =>
  replace(() => {
    const options: ExternalToast = {
      classNames: {
        content: classNames.detailContent,
        toast: classNames.detail,
      },
      duration,
    };
    return toast.custom(
      (id) => (
        // Sonner puts this in its title element, a block: the row is this element's own.
        <div className="flex items-start gap-3">
          <div className="flex min-w-0 flex-1 flex-col gap-1 wrap-break-word">
            <p className="type-toast text-toast-text m-0">{message}</p>
            {details.map((detail) => (
              <p key={detail} className="type-notice text-text-muted m-0">
                {detail}
              </p>
            ))}
            {warnings.map((warning) => (
              <p
                key={warning}
                className="type-notice text-tone-attention-fg m-0"
              >
                {warning}
              </p>
            ))}
          </div>
          {action !== undefined && (
            <button
              type="button"
              className={classNames.actionButton}
              onClick={() => {
                toast.dismiss(id);
                action.onClick();
              }}
            >
              {action.label}
            </button>
          )}
          <button
            type="button"
            className={classNames.actionButton}
            onClick={() => {
              toast.dismiss(id);
            }}
          >
            Dismiss
          </button>
        </div>
      ),
      options
    );
  });

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
  notifyDetail({ action, duration: 5000, message, warnings });

export interface ErrorToast {
  message: string;
  /** The CLI's fix, after the message: the toast then stays until dismissed. */
  hint?: string;
  /** More lines about what happened, under the message, in `type.notice` `text.muted`. */
  details?: string[];
  /** The command's warnings, under the error. */
  warnings?: string[];
  /** A way out, such as Add source, before Dismiss. */
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
    message: problemSentence(
      hint === undefined ? { message } : { hint, message },
      { keep }
    ),
    warnings,
  });

export const Toaster = () => (
  <Sonner
    position="bottom-center"
    // toast.offset
    offset={24}
    visibleToasts={1}
    toastOptions={{
      classNames: { actionButton: classNames.actionButton },
      unstyled: true,
    }}
  />
);
