/** Modifier symbols the mono font lacks; they fall back to a smaller system glyph. */
const symbols = new Set(["⌘", "⌥", "⇧", "⌃"]);

/**
 * A key or key combination such as `esc` or `⌘K` (DESIGN.md §6 "Key hint"): one entry in `keys`
 * per key, `key-hint.gap` apart. A modifier symbol is set in `type.key-hint-symbol`, so it reads as
 * large as the letter beside it.
 */
export const KeyHint = ({ keys }: { keys: string[] }) => (
  <kbd className="rounded-key-hint bg-button-secondary-bg type-key-hint text-text-muted px-key-hint py-key-hint-y gap-key-hint inline-flex items-center">
    {keys.map((key) => (
      <kbd
        key={key}
        className={symbols.has(key) ? "type-key-hint-symbol" : undefined}
      >
        {key}
      </kbd>
    ))}
  </kbd>
);
