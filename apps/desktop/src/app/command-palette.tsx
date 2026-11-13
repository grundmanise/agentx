import { useState } from "react";
import { useNavigate } from "react-router";

import { usePublishState } from "#/app/publish-status.tsx";
import { allScreens, SOURCES_PATH } from "#/app/screens.ts";
import { AddIcon } from "#/components/icons/add.tsx";
import { ArrowDownIcon } from "#/components/icons/arrow-down.tsx";
import { ArrowUpIcon } from "#/components/icons/arrow-up.tsx";
import { FolderIcon } from "#/components/icons/folder.tsx";
import type { IconComponent } from "#/components/icons/icon-props.ts";
import { ListIcon } from "#/components/icons/list.tsx";
import { PencilIcon } from "#/components/icons/pencil.tsx";
import { RefreshIcon } from "#/components/icons/refresh.tsx";
import { SkillsIcon } from "#/components/icons/skills.tsx";
import { notify, notifyError } from "#/components/toast.tsx";
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandShortcut,
} from "#/components/ui/command.tsx";
import { CreateSkillDialog } from "#/screens/skills/create-dialog.tsx";
import { openSkill } from "#/screens/skills/open.ts";
import { groupRows, originOf } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { publishAll } from "#/screens/skills/publish.ts";
import { useSkillRows } from "#/screens/skills/rows.ts";
import { checkUpdates, updateAll } from "#/screens/skills/updates.ts";
import { openActivityLog } from "#/store/activity-log.ts";
import { rescan } from "#/store/feed.ts";

export interface PaletteItem {
  /** Names the row among all others: two skills can share a name. */
  id: string;
  group: "ACTIONS" | "GO TO" | "SKILLS";
  label: string;
  hint?: string;
  icon: IconComponent;
  run: () => void;
}

/** The groups, top to bottom. */
const groupOrder: PaletteItem["group"][] = ["ACTIONS", "GO TO", "SKILLS"];

/** How many skills SKILLS lists while nothing is typed. */
const SKILLS_UNFILTERED = 4;

// A case-insensitive substring match on the label, which each row passes as its one keyword.
const filter = (value: string, search: string, keywords?: string[]) =>
  (keywords?.[0] ?? value).toLowerCase().includes(search.toLowerCase()) ? 1 : 0;

const skillsWord = (n: number) => (n === 1 ? "1 skill" : `${n} skills`);

/**
 * Rescan this machine: one refresh request to the serve child, then "Rescanned" once it answers,
 * or the error the scan failed with.
 */
const rescanMachine = async () => {
  const done = await rescan();
  if (done === undefined) {
    return;
  }
  if (done.ok) {
    notify("Rescanned");
  } else {
    notifyError({ message: done.error ?? "the scan failed" });
  }
};

/** SKILLS: every skill as the Skills list orders it, its origin as the hint. */
const skillItems = (rows: SkillRow[]): PaletteItem[] =>
  groupRows(rows, "asc")
    .flatMap((g) => g.rows)
    .map((row) => ({
      group: "SKILLS",
      hint: originOf(row),
      icon: SkillsIcon,
      id: `skill:${row.key}`,
      label: row.name,
      run: () => {
        openSkill(row.key);
      },
    }));

/**
 * The ⌘K palette: ACTIONS, GO TO and SKILLS. Update all shows while the store holds an update,
 * Publish all while the title bar says there are skills to publish. With nothing typed, SKILLS
 * lists the first four skills.
 */
export const CommandPalette = ({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const [creating, setCreating] = useState(false);
  // The palette opens empty: what was typed goes when it closes, however it closes.
  const [wasOpen, setWasOpen] = useState(open);
  if (open !== wasOpen) {
    setWasOpen(open);
    setSearch("");
  }
  const rows = useSkillRows();
  const publishState = usePublishState();
  const updates = rows.filter((r) => r.update !== undefined).length;

  const actions: PaletteItem[] = [];
  if (updates > 0) {
    actions.push({
      group: "ACTIONS",
      hint: skillsWord(updates),
      icon: ArrowDownIcon,
      id: "action:update-all",
      label: "Update all",
      run: () => {
        void updateAll();
      },
    });
  }
  if (publishState?.kind === "to-publish") {
    actions.push({
      group: "ACTIONS",
      hint: skillsWord(publishState.count),
      icon: ArrowUpIcon,
      id: "action:publish-all",
      label: "Publish all",
      run: () => {
        void publishAll();
      },
    });
  }
  actions.push(
    {
      group: "ACTIONS",
      icon: AddIcon,
      id: "action:add-skills",
      label: "Add skills",
      run: () => {
        void navigate(SOURCES_PATH);
      },
    },
    {
      group: "ACTIONS",
      icon: PencilIcon,
      id: "action:create-skill",
      label: "Create skill",
      run: () => {
        setCreating(true);
      },
    },
    {
      group: "ACTIONS",
      icon: RefreshIcon,
      id: "action:check-updates",
      label: "Check updates",
      run: () => {
        void checkUpdates();
      },
    },
    {
      group: "ACTIONS",
      icon: FolderIcon,
      id: "action:rescan",
      label: "Rescan this machine",
      run: () => {
        void rescanMachine();
      },
    },
    {
      group: "ACTIONS",
      icon: ListIcon,
      id: "action:activity-log",
      label: "Show activity log",
      run: () => {
        openActivityLog();
      },
    }
  );
  const goTo: PaletteItem[] = allScreens.map((s) => ({
    group: "GO TO",
    icon: s.icon,
    id: `go:${s.path}`,
    label: s.label,
    run: () => {
      void navigate(s.path);
    },
  }));
  const skills = skillItems(rows);
  const items = [
    ...actions,
    ...goTo,
    ...(search === "" ? skills.slice(0, SKILLS_UNFILTERED) : skills),
  ];

  return (
    <>
      <CommandDialog
        open={open}
        onOpenChange={onOpenChange}
        filter={filter}
        loop
      >
        <CommandInput
          autoFocus
          placeholder="Search skills or run a command"
          escapeHint
          value={search}
          onValueChange={setSearch}
        />
        <CommandList>
          <CommandEmpty>No matches</CommandEmpty>
          {groupOrder
            .filter((g) => items.some((i) => i.group === g))
            .map((g) => (
              <CommandGroup key={g} heading={g}>
                {items
                  .filter((i) => i.group === g)
                  .map(({ icon: RowIcon, ...i }) => (
                    <CommandItem
                      key={i.id}
                      value={i.id}
                      keywords={[i.label]}
                      onSelect={() => {
                        onOpenChange(false);
                        i.run();
                      }}
                    >
                      {/* The row icon turns to the accent while its row is selected. */}
                      <span className="text-palette-icon group-data-[selected=true]:text-palette-icon-active flex">
                        <RowIcon size="sm" />
                      </span>
                      <span className="type-body flex-1 truncate">
                        {i.label}
                      </span>
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
      <CreateSkillDialog open={creating} onOpenChange={setCreating} />
    </>
  );
};
