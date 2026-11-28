// Import skills, in the Hub card of Settings: one button per tool that installed skills agentx can
// adopt (the inventory's `adoptable`), each opening the adopt dialog for that tool's skills. With
// none to adopt, a line under the row says so, and the row stays.
import { useState } from "react";

import { Button } from "#/components/ui/button.tsx";
import { SettingsRow } from "#/screens/settings/section.tsx";
import { AdoptDialog } from "#/screens/skills/adopt-dialog.tsx";
import { toolsToAdopt, useAdoptRun } from "#/screens/skills/adopt.ts";
import type { ToolArg } from "#/screens/skills/adopt.ts";
import { useAppStore } from "#/store/store.ts";

export const AdoptSkillsRow = () => {
  const inventory = useAppStore((s) => s.inventory);
  const busy = useAdoptRun((s) => s.run !== undefined);
  // The tool whose skills the dialog adopts. It stays while the dialog closes.
  const [adopting, setAdopting] = useState<ToolArg>();
  const [open, setOpen] = useState(false);
  const tools = toolsToAdopt(inventory);
  return (
    <SettingsRow
      name="Import skills"
      line="Let agentx manage the skills another tool installed."
      control={
        tools.length === 0
          ? undefined
          : tools.map((tool) => (
              <Button
                key={tool.arg}
                variant="secondary"
                size="sm"
                track="settings.adopt-import"
                disabled={busy}
                onClick={() => {
                  setAdopting(tool.arg);
                  setOpen(true);
                }}
              >
                {`Import from ${tool.name}`}
              </Button>
            ))
      }
    >
      {tools.length === 0 && (
        <p className="type-secondary text-text-hint m-0">
          No skills from the Skills CLI or Skillshare to import
        </p>
      )}
      <AdoptDialog
        tools={adopting === undefined ? [] : [adopting]}
        open={open}
        onOpenChange={(next) => {
          if (!next) {
            setOpen(false);
          }
        }}
      />
    </SettingsRow>
  );
};
