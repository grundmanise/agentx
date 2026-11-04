// shadcn/ui Command (`pnpm dlx shadcn@latest add command`), on our tokens: DESIGN.md §2.2
// "Overlays" and §4 "Overlays", the palette. Its parts keep shadcn's names and `data-slot`
// attributes and take no `className`.
import { Command as CommandPrimitive } from "cmdk";
import type { ReactNode } from "react";

import { SearchIcon } from "#/components/icons/search.tsx";
import { KeyHint } from "#/components/key-hint.tsx";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import type { OwnProps } from "#/lib/utils.ts";

const Command = ({ ...props }: OwnProps<typeof CommandPrimitive>) => (
  <CommandPrimitive
    data-slot="command"
    className="flex h-full w-full flex-col overflow-hidden"
    {...props}
  />
);

/** A Command in a dialog placed as the palette, named for assistive technology only. */
const CommandDialog = ({
  title = "Command palette",
  description = "Search for a command to run",
  filter,
  loop,
  children,
  ...props
}: OwnProps<typeof Dialog> &
  Pick<OwnProps<typeof CommandPrimitive>, "filter" | "loop"> & {
    title?: string;
    description?: string;
    children: ReactNode;
  }) => (
  <Dialog {...props}>
    <DialogContent kind="palette">
      <div className="sr-only">
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </div>
      <Command filter={filter} loop={loop}>
        {children}
      </Command>
    </DialogContent>
  </Dialog>
);

/**
 * The search row, `palette.input-height` high: the magnifier in `palette.icon`, the input in
 * `type.palette-input`, and `esc` as a key hint when the row is in a dialog.
 */
const CommandInput = ({
  escapeHint = false,
  ...props
}: OwnProps<typeof CommandPrimitive.Input> & { escapeHint?: boolean }) => (
  <div
    data-slot="command-input-wrapper"
    className="border-card-border h-palette-input flex items-center gap-3 border-b px-5"
  >
    <span className="text-palette-icon flex">
      <SearchIcon size="lg" />
    </span>
    <CommandPrimitive.Input
      data-slot="command-input"
      className="type-palette-input h-full flex-1 border-0 bg-transparent outline-none focus-visible:outline-none"
      {...props}
    />
    {escapeHint && <KeyHint>esc</KeyHint>}
  </div>
);

const CommandList = ({ ...props }: OwnProps<typeof CommandPrimitive.List>) => (
  <CommandPrimitive.List
    data-slot="command-list"
    className="max-h-palette-list overflow-x-hidden overflow-y-auto p-2"
    {...props}
  />
);

const CommandEmpty = ({
  ...props
}: OwnProps<typeof CommandPrimitive.Empty>) => (
  <CommandPrimitive.Empty
    data-slot="command-empty"
    className="text-text-muted p-7 text-center"
    {...props}
  />
);

/** A group under its heading in `type.caption`, `text.muted`. */
const CommandGroup = ({
  ...props
}: OwnProps<typeof CommandPrimitive.Group>) => (
  <CommandPrimitive.Group
    data-slot="command-group"
    className="[&_[cmdk-group-heading]]:type-caption [&_[cmdk-group-heading]]:text-text-muted [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:pt-3 [&_[cmdk-group-heading]]:pb-1.5"
    {...props}
  />
);

const CommandSeparator = ({
  ...props
}: OwnProps<typeof CommandPrimitive.Separator>) => (
  <CommandPrimitive.Separator
    data-slot="command-separator"
    className="bg-card-border -mx-2 h-px"
    {...props}
  />
);

/**
 * A row in `palette.row-text`, `palette.row-active` while selected. It is a `group`: a child
 * can follow its selection with `group-data-[selected=true]:`.
 */
const CommandItem = ({ ...props }: OwnProps<typeof CommandPrimitive.Item>) => (
  <CommandPrimitive.Item
    data-slot="command-item"
    className="group rounded-palette-row text-palette-row-text data-[selected=true]:bg-palette-row-active data-[selected=true]:text-text-primary flex h-10.5 items-center gap-3 px-3 outline-none select-none data-[disabled=true]:pointer-events-none data-[disabled=true]:opacity-30"
    {...props}
  />
);

/** The hint at the end of a row, in `type.palette-hint`; `text.muted` while the row is selected. */
const CommandShortcut = ({ ...props }: OwnProps<"span">) => (
  <span
    data-slot="command-shortcut"
    className="type-palette-hint text-text-caption group-data-[selected=true]:text-text-muted ml-auto"
    {...props}
  />
);

export {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
  CommandShortcut,
};
