// shadcn/ui Kbd and KbdGroup (`pnpm dlx shadcn@latest add kbd`), on our tokens: DESIGN.md §6
// "Key hint". They keep shadcn's parts and `data-slot` attributes and take no `className`. Our key
// hint is one pill round the keys of a combination, so the look is on KbdGroup: `key-hint.padding`,
// `key-hint.radius`, `button.secondary-bg`, `type.key-hint` `text.muted`, its keys `key-hint.gap`
// apart. Each Kbd is one key in it, so `⌘K` and `Ctrl K` space their keys the same way. A key the
// mono font lacks, such as `⌘`, is our icon with its glyph in `sr-only` text.
import type { OwnProps } from "#/lib/utils.ts";

const Kbd = (props: OwnProps<"kbd">) => (
  <kbd data-slot="kbd" className="flex" {...props} />
);

const KbdGroup = (props: OwnProps<"kbd">) => (
  <kbd
    data-slot="kbd-group"
    className="rounded-key-hint bg-button-secondary-bg type-key-hint text-text-muted px-key-hint py-key-hint-y gap-key-hint inline-flex items-center"
    {...props}
  />
);

export { Kbd, KbdGroup };
