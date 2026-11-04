import { useNavigate } from "react-router";

import { screens } from "#/app/screens.ts";
import type { IconComponent } from "#/components/icons/icon-props.ts";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "#/components/ui/command.tsx";

export interface PaletteItem {
  group: "ACTIONS" | "GO TO" | "SKILLS";
  label: string;
  hint?: string;
  icon: IconComponent;
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
    <CommandDialog open={open} onOpenChange={onOpenChange} filter={filter} loop>
      <CommandInput
        autoFocus
        placeholder="Search skills or run a command"
        escapeHint
      />
      <CommandList>
        <CommandEmpty>No matches</CommandEmpty>
        {groups.map((g) => (
          <CommandGroup key={g} heading={g}>
            {items
              .filter((i) => i.group === g)
              .map(({ icon: RowIcon, ...i }) => (
                <CommandItem
                  key={i.label}
                  value={i.label}
                  onSelect={() => {
                    onOpenChange(false);
                    i.run();
                  }}
                >
                  {/* The row icon turns to the accent while its row is selected. */}
                  <span className="text-palette-icon group-data-[selected=true]:text-palette-icon-active flex">
                    <RowIcon size="sm" />
                  </span>
                  <span className="type-body flex-1">{i.label}</span>
                  {i.hint !== undefined && (
                    <CommandShortcut>{i.hint}</CommandShortcut>
                  )}
                </CommandItem>
              ))}
          </CommandGroup>
        ))}
      </CommandList>
      <div className="border-card-border type-palette-footer text-text-muted flex gap-4.5 border-t px-5 py-3">
        <span>
          <span className="type-palette-hint text-text-muted">↑↓</span> move
        </span>
        <span>
          <span className="type-palette-hint text-text-muted">↵</span> run
        </span>
      </div>
    </CommandDialog>
  );
};
