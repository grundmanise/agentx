import { toast, Toaster as Sonner } from "sonner";

import { cn } from "#/lib/utils.ts";

// The toast last shown, which the next one replaces.
let shown: string | number | undefined;

/**
 * One toast at a time (DESIGN.md §6 "Toast"). A toast with an action stays 5s,
 * one without 2.6s. A new toast replaces the one showing.
 */
export const notify = (
  message: string,
  action?: { label: string; onClick: () => void }
) => {
  const previous = shown;
  shown = toast(message, {
    action: action && { label: action.label, onClick: action.onClick },
    duration: action ? 5000 : 2600,
  });
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

// Wrapped in cn() so the design-system lint reads these strings as classes.
const classNames = {
  actionButton: cn(
    "rounded-button type-toast-action text-button-toast-action-text hover:bg-button-ghost-hover h-7.5 px-3.5"
  ),
  title: cn("pr-3"),
  toast: cn(
    "rounded-toast border-overlay-border bg-overlay-bg type-toast text-toast-text shadow-popover h-toast pl-toast flex items-center gap-3 border pr-1.5 whitespace-nowrap"
  ),
};

export const Toaster = () => (
  <Sonner
    position="bottom-center"
    // toast.offset
    offset={24}
    visibleToasts={1}
    toastOptions={{ classNames, unstyled: true }}
  />
);
