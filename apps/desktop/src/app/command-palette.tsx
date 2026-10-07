import * as Dialog from "@radix-ui/react-dialog";
import { Command } from "cmdk";
import { useNavigate } from "react-router";
import { Icon } from '#/components/icon.tsx';
import type { IconName } from '#/components/icon.tsx';
import { KeyHint } from "#/components/key-hint.tsx";
import { screens } from "#/app/screens.ts";

export interface PaletteItem {
  group: "ACTIONS" | "GO TO" | "SKILLS";
  label: string;
  hint?: string;
  icon: IconName;
  run: () => void;
}

// A case-insensitive substring match on the label.
const filter = (value: string, search: string) => (value.toLowerCase().includes(search.toLowerCase()) ? 1 : 0);

/**
 * The ⌘K palette. This change wires GO TO only; ACTIONS and SKILLS come with the screens and
 * CLI commands they run.
 */
export const CommandPalette = ({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) => {
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
        <Dialog.Overlay className="fixed inset-0 z-50 bg-shell-scrim-strong" />
        <Dialog.Content
          aria-describedby={undefined}
          className="fixed top-(--palette-top) left-1/2 z-50 flex w-(--palette-width) -translate-x-1/2 flex-col overflow-hidden rounded-palette border border-overlay-border bg-overlay-bg shadow-overlay"
        >
          <Dialog.Title className="sr-only">Command palette</Dialog.Title>
          <Command filter={filter} loop>
            <div className="flex h-(--palette-input-height) items-center gap-3 border-b border-card-border px-5">
              <Icon name="search" size={18} strokeWidth={2} tone="palette" />
              <Command.Input
                autoFocus
                placeholder="Search skills or run a command"
                className="h-full flex-1 border-0 bg-transparent type-palette-input outline-none focus-visible:outline-none"
              />
              <KeyHint>esc</KeyHint>
            </div>
            <Command.List className="max-h-(--palette-list-max-height) overflow-auto p-2">
              <Command.Empty className="p-7 text-center text-text-muted">No matches</Command.Empty>
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
                        className="group flex h-10.5 items-center gap-3 rounded-palette-row px-3 text-palette-row-text data-[selected=true]:bg-palette-row-active data-[selected=true]:text-text-primary"
                      >
                        <Icon name={i.icon} size={15} tone="palette-row" />
                        <span className="flex-1 type-body">{i.label}</span>
                        {i.hint !== undefined && <span className="type-palette-hint text-text-caption">{i.hint}</span>}
                      </Command.Item>
                    ))}
                </Command.Group>
              ))}
            </Command.List>
          </Command>
          <div className="flex gap-4.5 border-t border-card-border px-5 py-3 type-palette-footer text-text-muted">
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
