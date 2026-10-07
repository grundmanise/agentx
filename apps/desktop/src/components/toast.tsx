import { toast, Toaster as Sonner } from "sonner";
import { cn } from "#/lib/utils.ts";

/**
 * One toast at a time (DESIGN.md §6 "Toast"). A toast with an action stays 5s,
 * one without 2.6s. A new toast replaces the one showing.
 */
export const notify = (message: string, action?: { label: string; onClick: () => void }) => {
  toast.dismiss();
  return toast(message, {
    action: action && { label: action.label, onClick: action.onClick },
    duration: action ? 5000 : 2600,
  });
};

// Wrapped in cn() so the design-system lint reads these strings as classes.
const classNames = {
  actionButton: cn(
    "h-7.5 rounded-button px-3.5 type-toast-action text-button-toast-action-text hover:bg-button-ghost-hover",
  ),
  title: cn("pr-3"),
  toast: cn(
    "flex h-(--toast-height) items-center gap-3 whitespace-nowrap rounded-toast border border-overlay-border bg-overlay-bg pr-1.5 pl-4.5 type-toast text-toast-text shadow-popover",
  ),
};

export const Toaster = () => 
  (
    <Sonner
      position="bottom-center"
      // toast.offset
      offset={24}
      visibleToasts={1}
      toastOptions={{ classNames, unstyled: true }}
    />
  )
;
