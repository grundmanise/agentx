import type { ReactNode } from "react";

/** A keyboard key such as `esc` or `⌘K` (DESIGN.md §6 "Key hint"). */
export function KeyHint({ children }: { children: ReactNode }) {
  return (
    <kbd className="rounded-key-hint bg-button-secondary-bg p-(--key-hint-padding) type-key-hint text-text-muted">
      {children}
    </kbd>
  );
}
