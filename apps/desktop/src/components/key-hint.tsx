import type { ReactNode } from "react";

import { CommandKeyIcon } from "#/components/icons/command-key.tsx";

/** Keys drawn as an icon, since the mono font lacks their glyph. */
const icons: Record<string, ReactNode> = { "⌘": <CommandKeyIcon /> };

/**
 * A key or key combination such as `esc` or `⌘K` (DESIGN.md §6 "Key hint"): one entry in `keys`
 * per key, `key-hint.gap` apart.
 */
export const KeyHint = ({ keys }: { keys: string[] }) => (
  <kbd className="rounded-key-hint bg-button-secondary-bg type-key-hint text-text-muted px-key-hint py-key-hint-y gap-key-hint inline-flex items-center">
    {keys.map((key) =>
      key in icons ? (
        <kbd key={key} className="flex">
          {icons[key]}
          <span className="sr-only">{key}</span>
        </kbd>
      ) : (
        <kbd key={key}>{key}</kbd>
      )
    )}
  </kbd>
);
