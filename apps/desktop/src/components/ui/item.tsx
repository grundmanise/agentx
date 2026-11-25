// shadcn/ui Item (`pnpm dlx shadcn@latest add item`), on our tokens: DESIGN.md §6 "Settings". It
// keeps shadcn's parts and their `data-slot` attributes and takes no `className`. Only the parts a
// Settings row uses: the item, a wrapping flex row; its content, the title over the description;
// its actions at the right; and its footer, what shows under the row on a line of its own. With
// `asChild`, the item renders its child, such as a `label`, with the item's look.
import { Slot } from "radix-ui";

import type { OwnProps } from "#/lib/utils.ts";

const Item = ({
  asChild = false,
  ...props
}: OwnProps<"div"> & { asChild?: boolean }) => {
  const Comp = asChild ? Slot.Root : "div";
  return (
    <Comp
      data-slot="item"
      className="py-settings-row gap-y-settings-row flex flex-wrap items-center gap-x-6"
      {...props}
    />
  );
};

const ItemContent = (props: OwnProps<"div">) => (
  <div
    data-slot="item-content"
    className="flex min-w-0 flex-1 flex-col gap-1"
    {...props}
  />
);

const ItemTitle = (props: OwnProps<"div">) => (
  <div
    data-slot="item-title"
    className="type-body text-text-primary"
    {...props}
  />
);

const ItemDescription = (props: OwnProps<"p">) => (
  <p
    data-slot="item-description"
    className="type-secondary text-text-muted m-0"
    {...props}
  />
);

/** The controls at the right, up to three fifths of the row. */
const ItemActions = (props: OwnProps<"div">) => (
  <div
    data-slot="item-actions"
    className="flex max-w-3/5 flex-none flex-wrap items-center justify-end gap-2"
    {...props}
  />
);

/** What shows under the row; hidden while it holds nothing, such as a closed dialog. */
const ItemFooter = (props: OwnProps<"div">) => (
  <div
    data-slot="item-footer"
    className="flex basis-full flex-col empty:hidden"
    {...props}
  />
);

export {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemFooter,
  ItemTitle,
};
