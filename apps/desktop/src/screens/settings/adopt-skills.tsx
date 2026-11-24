// The Adopt skills section of Settings: one button per tool that installed skills agentx can
// adopt (the adoption preview's candidates), each opening the adopt dialog for that tool's
// skills. With none to adopt, one muted line says so, and the section stays. Opening Settings reads
// the preview again, so a skill another tool installed while the app ran shows up.
import { useEffect, useState } from "react";

import { Button } from "#/components/ui/button.tsx";
import { SettingsSection } from "#/screens/settings/section.tsx";
import { AdoptDialog } from "#/screens/skills/adopt-dialog.tsx";
import { toolsToAdopt, useAdoptRun } from "#/screens/skills/adopt.ts";
import type { ToolArg } from "#/screens/skills/adopt.ts";
import { loadAdoption } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";

export const AdoptSkillsSection = () => {
  const adoption = useAppStore((s) => s.adoption);
  const busy = useAdoptRun((s) => s.run !== undefined);
  // The tool whose skills the dialog adopts. It stays while the dialog closes.
  const [adopting, setAdopting] = useState<ToolArg>();
  const [open, setOpen] = useState(false);
  useEffect(() => {
    void loadAdoption();
  }, []);
  const tools = toolsToAdopt(adoption);
  return (
    <SettingsSection
      title="Import skills"
      description="Let agentx manage the skills another tool installed."
    >
      {tools.length === 0 ? (
        <p className="type-secondary text-text-muted m-0">
          No skills from the Skills CLI or Skillshare to import
        </p>
      ) : (
        <div className="gap-button flex flex-wrap items-center">
          {tools.map((tool) => (
            <Button
              key={tool.arg}
              variant="secondary"
              track="settings.adopt-import"
              disabled={busy}
              onClick={() => {
                setAdopting(tool.arg);
                setOpen(true);
              }}
            >
              {`Import skills from ${tool.name}`}
            </Button>
          ))}
        </div>
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
    </SettingsSection>
  );
};
