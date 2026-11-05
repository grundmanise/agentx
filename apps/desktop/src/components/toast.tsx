import { toast, Toaster as Sonner } from "sonner";
import type { ExternalToast } from "sonner";

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
  toast: cn(
    "rounded-toast border-overlay-border bg-overlay-bg type-toast text-toast-text shadow-popover h-toast pl-toast flex items-center gap-3 border pr-1.5 whitespace-nowrap"
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

/**
 * One toast at a time (DESIGN.md §6 "Toast"). A toast with an action stays 5s, one without 2.6s,
 * and a `lasting` one, for a command that runs with its Cancel, until the next toast replaces it.
 * A new toast replaces the one showing.
 */
export const notify = (
  message: string,
  action?: { label: string; onClick: () => void },
  { lasting = false }: { lasting?: boolean } = {}
) =>
  replace(() =>
    toast(message, {
      action: action && { label: action.label, onClick: action.onClick },
      classNames: { title: classNames.title, toast: classNames.toast },
      duration: durationOf(action !== undefined, lasting),
    })
  );

export interface ErrorToast {
  message: string;
  /** The CLI's fix: the toast then stays until dismissed. */
  hint?: string;
  /** The command's warnings, under the error. */
  warnings?: string[];
}

/**
 * A command's error (DESIGN.md §6 "Error toast"): the CLI's message, its hint under it, then the
 * command's warnings. It stays until dismissed when it has a hint, and 5s when it has none.
 */
export const notifyError = ({ message, hint, warnings = [] }: ErrorToast) =>
  replace(() => {
    const options: ExternalToast = {
      classNames: {
        content: classNames.detailContent,
        toast: classNames.detail,
      },
      duration: hint === undefined ? 5000 : Number.POSITIVE_INFINITY,
    };
    return toast.custom(
      (id) => (
        // Sonner puts this in its title element, a block: the row is this element's own.
        <div className="flex items-start gap-3">
          <div className="flex min-w-0 flex-1 flex-col gap-1 wrap-break-word">
            <p className="type-toast text-toast-text m-0">{message}</p>
            {hint !== undefined && (
              <p className="type-notice text-text-muted m-0">{hint}</p>
            )}
            {warnings.map((warning) => (
              <p
                key={warning}
                className="type-notice text-tone-attention-fg m-0"
              >
                {warning}
              </p>
            ))}
          </div>
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
