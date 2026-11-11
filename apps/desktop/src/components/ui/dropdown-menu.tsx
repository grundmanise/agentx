// shadcn/ui Dropdown Menu (`pnpm dlx shadcn@latest add dropdown-menu`), on our tokens: DESIGN.md
// §6 "Menu". Its parts keep shadcn's names and `data-slot` attributes and take no `className`;
// its animation classes are dropped (DESIGN.md §8). The design has no checkbox, radio or
// nested menus and no shortcuts, so those parts are left out until a design needs them.
import { DropdownMenu as DropdownMenuPrimitive } from "radix-ui";
import type { ReactNode } from "react";

import type { OwnProps } from "#/lib/utils.ts";

/**
 * Not modal (DESIGN.md §6 "Menu"): the page under the menu stays live, so opening and closing it
 * costs the same on a page of any size. Radix's modal menu makes the rest of the page inert, which
 * restyles every element when it opens and again when it closes.
 */
const DropdownMenu = ({
  ...props
}: OwnProps<typeof DropdownMenuPrimitive.Root>) => (
  <DropdownMenuPrimitive.Root
    data-slot="dropdown-menu"
    modal={false}
    {...props}
  />
);

const DropdownMenuPortal = ({
  ...props
}: OwnProps<typeof DropdownMenuPrimitive.Portal>) => (
  <DropdownMenuPrimitive.Portal data-slot="dropdown-menu-portal" {...props} />
);

const DropdownMenuTrigger = ({
  ...props
}: OwnProps<typeof DropdownMenuPrimitive.Trigger>) => (
  <DropdownMenuPrimitive.Trigger data-slot="dropdown-menu-trigger" {...props} />
);

/**
 * `menu.min-width`, `menu.radius`, `overlay.bg` with `overlay.border`, `menu.shadow` and
 * `menu.padding`, `menu.offset` from its trigger, aligned to its end by default.
 */
const DropdownMenuContent = ({
  align = "end",
  // menu.offset
  sideOffset = 4,
  ...props
}: OwnProps<typeof DropdownMenuPrimitive.Content>) => (
  <DropdownMenuPrimitive.Portal>
    <DropdownMenuPrimitive.Content
      data-slot="dropdown-menu-content"
      align={align}
      sideOffset={sideOffset}
      className="rounded-menu border-overlay-border bg-overlay-bg shadow-menu min-w-menu p-menu z-50 flex flex-col overflow-y-auto border outline-none"
      {...props}
    />
  </DropdownMenuPrimitive.Portal>
);

const DropdownMenuGroup = ({
  ...props
}: OwnProps<typeof DropdownMenuPrimitive.Group>) => (
  <DropdownMenuPrimitive.Group data-slot="dropdown-menu-group" {...props} />
);

/** A row of `menu.row-radius`, `menu.row-hover` when pointed at or focused; its lines stack. */
const DropdownMenuItem = ({
  ...props
}: OwnProps<typeof DropdownMenuPrimitive.Item>) => (
  <DropdownMenuPrimitive.Item
    data-slot="dropdown-menu-item"
    className="rounded-menu-row focus:bg-menu-row-hover flex cursor-default flex-col items-start gap-0.5 px-2.5 py-2 outline-none select-none data-disabled:pointer-events-none data-disabled:opacity-30"
    {...props}
  />
);

/** A caption over a group of rows, `type.caption` in `text.caption`. */
const DropdownMenuLabel = ({ children }: { children: ReactNode }) => (
  <DropdownMenuPrimitive.Label
    data-slot="dropdown-menu-label"
    className="type-caption text-text-caption px-2.5 pt-1.5 pb-1"
  >
    {children}
  </DropdownMenuPrimitive.Label>
);

const DropdownMenuSeparator = ({
  ...props
}: OwnProps<typeof DropdownMenuPrimitive.Separator>) => (
  <DropdownMenuPrimitive.Separator
    data-slot="dropdown-menu-separator"
    className="bg-overlay-border my-1 h-px"
    {...props}
  />
);

export {
  DropdownMenu,
  DropdownMenuPortal,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuLabel,
  DropdownMenuItem,
  DropdownMenuSeparator,
};
