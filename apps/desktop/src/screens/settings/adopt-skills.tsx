// The Adopt skills section of Settings: one button per tool that installed skills agentx can
// adopt (the adoption preview's candidates), each opening the adopt dialog for that tool's
// skills. With none to adopt, one muted line says so, and the section stays.
import { useState } from "react";

import { Button } from "#/components/ui/button.tsx";
import { SettingsSection } from "#/screens/settings/section.tsx";
import { AdoptDialog } from "#/screens/skills/adopt-dialog.tsx";
import { toolsToAdopt, useAdoptRun } from "#/screens/skills/adopt.ts";
import type { ToolArg } from "#/screens/skills/adopt.ts";
import { useAppStore } from "#/store/store.ts";

export const AdoptSkillsSection = () => {
  const adoption = useAppStore((s) => s.adoption);
  const busy = useAdoptRun((s) => s.run !== undefined);
  // The tool whose skills the dialog adopts, while it is open.
  const [open, setOpen] = useState<ToolArg | undefined>();
  const tools = toolsToAdopt(adoption);
  return (
    <SettingsSection
      title="Adopt skills"
      description="Let agentx manage the skills another tool installed."
    >
      {tools.length === 0 ? (
        <p className="type-secondary text-text-muted m-0">
          No skills from the Skills CLI or Skillshare to adopt
        </p>
      ) : (
        <div className="gap-button flex flex-wrap items-center">
          {tools.map((tool) => (
            <Button
              key={tool.arg}
              variant="secondary"
              disabled={busy}
              onClick={() => {
                setOpen(tool.arg);
              }}
            >
              {`Adopt ${tool.name} skills`}
            </Button>
          ))}
        </div>
      )}
      <AdoptDialog
        tools={open === undefined ? [] : [open]}
        open={open !== undefined}
        onOpenChange={(next) => {
          if (!next) {
            setOpen(undefined);
          }
        }}
      />
    </SettingsSection>
  );
};
