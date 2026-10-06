import { toast, Toaster as Sonner } from "sonner";

/**
 * One toast at a time (00 app shell, DESIGN.md §6 "Toast"). A toast with an action stays 5s,
 * one without 2.6s. A new toast replaces the one showing.
 */
export function notify(message: string, action?: { label: string; onClick: () => void }) {
  toast.dismiss();
  return toast(message, {
    duration: action ? 5000 : 2600,
    action: action && { label: action.label, onClick: action.onClick },
  });
}

export function Toaster() {
  return (
    <Sonner
      position="bottom-center"
      offset={24}
      visibleToasts={1}
      toastOptions={{
        unstyled: true,
        classNames: {
          toast:
            "flex h-(--toast-height) items-center gap-3 whitespace-nowrap rounded-toast border border-overlay-border bg-overlay-bg pr-1.5 pl-[18px] text-[13.5px] text-toast-text shadow-popover",
          title: "pr-3",
          actionButton:
            "h-[30px] rounded-button px-3.5 text-[13px] font-semibold text-button-toast-action-text hover:bg-button-ghost-hover",
        },
      }}
    />
  );
}
