/**
 * A key or key combination such as `esc` or `⌘K` (DESIGN.md §6 "Key hint"): one entry in `keys`
 * per key, `key-hint.gap` apart.
 */
export const KeyHint = ({ keys }: { keys: string[] }) => (
  <kbd className="rounded-key-hint bg-button-secondary-bg type-key-hint text-text-muted px-key-hint py-key-hint-y gap-key-hint inline-flex items-center">
    {keys.map((key) => (
      <kbd key={key}>{key}</kbd>
    ))}
  </kbd>
);
