import type { ReactNode } from "react";

/** A keyboard key such as `esc` or `⌘K` (DESIGN.md §6 "Key hint"). */
export const KeyHint = ({ children }: { children: ReactNode }) => (
  <kbd className="rounded-key-hint bg-button-secondary-bg type-key-hint text-text-muted p-(--key-hint-padding)">
    {children}
  </kbd>
);
