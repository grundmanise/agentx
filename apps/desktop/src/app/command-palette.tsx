import * as Dialog from "@radix-ui/react-dialog";
import { Command } from "cmdk";
import { useNavigate } from "react-router";

import { screens } from "#/app/screens.ts";
import { Icon } from "#/components/icon.tsx";
import type { IconName } from "#/components/icon.tsx";
import { KeyHint } from "#/components/key-hint.tsx";

export interface PaletteItem {
  group: "ACTIONS" | "GO TO" | "SKILLS";
  label: string;
  hint?: string;
  icon: IconName;
  run: () => void;
}

// A case-insensitive substring match on the label.
const filter = (value: string, search: string) =>
  value.toLowerCase().includes(search.toLowerCase()) ? 1 : 0;

/**
 * The ⌘K palette. This change wires GO TO only; ACTIONS and SKILLS come with the screens and
 * CLI commands they run.
 */
export const CommandPalette = ({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const navigate = useNavigate();
  const items: PaletteItem[] = screens.map((s) => ({
    group: "GO TO",
    icon: s.icon,
    label: s.label,
    run: () => {
      void navigate(s.path);
    },
  }));
  const groups = [...new Set(items.map((i) => i.group))];

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="bg-shell-scrim-strong fixed inset-0 z-50" />
        <Dialog.Content
          aria-describedby={undefined}
          className="rounded-palette border-overlay-border bg-overlay-bg shadow-overlay top-palette w-palette fixed left-1/2 z-50 flex -translate-x-1/2 flex-col overflow-hidden border"
        >
          <Dialog.Title className="sr-only">Command palette</Dialog.Title>
          <Command filter={filter} loop>
            <div className="border-card-border h-palette-input flex items-center gap-3 border-b px-5">
              <Icon name="search" size={18} strokeWidth={2} tone="palette" />
              <Command.Input
                autoFocus
                placeholder="Search skills or run a command"
                className="type-palette-input h-full flex-1 border-0 bg-transparent outline-none focus-visible:outline-none"
              />
              <KeyHint>esc</KeyHint>
            </div>
            <Command.List className="max-h-palette-list overflow-auto p-2">
              <Command.Empty className="text-text-muted p-7 text-center">
                No matches
              </Command.Empty>
              {groups.map((g) => (
                <Command.Group
                  key={g}
                  heading={g}
                  className="[&_[cmdk-group-heading]]:type-caption [&_[cmdk-group-heading]]:text-text-caption [&_[cmdk-group-heading]]:px-3 [&_[cmdk-group-heading]]:pt-3 [&_[cmdk-group-heading]]:pb-1.5"
                >
                  {items
                    .filter((i) => i.group === g)
                    .map((i) => (
                      <Command.Item
                        key={i.label}
                        value={i.label}
                        onSelect={() => {
                          onOpenChange(false);
                          i.run();
                        }}
                        className="group rounded-palette-row text-palette-row-text data-[selected=true]:bg-palette-row-active data-[selected=true]:text-text-primary flex h-10.5 items-center gap-3 px-3"
                      >
                        <Icon name={i.icon} size={15} tone="palette-row" />
                        <span className="type-body flex-1">{i.label}</span>
                        {i.hint !== undefined && (
                          <span className="type-palette-hint text-text-caption">
                            {i.hint}
                          </span>
                        )}
                      </Command.Item>
                    ))}
                </Command.Group>
              ))}
            </Command.List>
          </Command>
          <div className="border-card-border type-palette-footer text-text-muted flex gap-4.5 border-t px-5 py-3">
            <span>
              <span className="type-palette-hint text-text-muted">↑↓</span> move
            </span>
            <span>
              <span className="type-palette-hint text-text-muted">↵</span> run
            </span>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
};
