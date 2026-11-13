// The Remove dialog of a shared source on the Sources screen: Remove runs `source remove <url>`,
// and when the user also chose the skills this machine installed from it, `skill remove <name>`
// for each of them first, one after another, stopping at the first that fails. There is no Undo:
// the skills removed are gone, and the source is added again with Add source.
import { useId, useState } from "react";

import type { LibraryEntry, SourceEvent } from "#/cli/events.gen.ts";
import { Button } from "#/components/ui/button.tsx";
import { Checkbox } from "#/components/ui/checkbox.tsx";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import { shortSource } from "#/screens/skills/projection.ts";
import {
  count,
  installedFrom,
  removedText,
} from "#/screens/sources/projection.ts";
import { runCommand } from "#/store/runner.ts";

/** The pending label of the removal of the source `id`, with its skills when chosen. */
export const removeSourceLabel = (id: string) => `remove source ${id}`;

/** Removes `skills`, then the source; the toast names what went. */
const removeSource = async (source: SourceEvent, skills: string[]) => {
  const label = removeSourceLabel(source.id);
  for (const name of skills) {
    // oxlint-disable-next-line no-await-in-loop -- one command at a time, in order
    const outcome = await runCommand(["skill", "remove", name], { label });
    if (outcome.exitCode !== 0) {
      return;
    }
  }
  await runCommand(["source", "remove", source.url], {
    label,
    success: removedText(skills.length),
  });
};

export const RemoveSourceDialog = ({
  source,
  library,
  open,
  onOpenChange,
}: {
  source: SourceEvent;
  library: LibraryEntry[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const [withSkills, setWithSkills] = useState(false);
  const checkboxId = useId();
  const installed = installedFrom(source.url, library);
  // The choice lasts one confirmation: a dialog opened again starts unticked.
  const close = () => {
    setWithSkills(false);
    onOpenChange(false);
  };
  const remove = () => {
    void removeSource(source, withSkills ? installed : []);
    close();
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (next) {
          onOpenChange(true);
        } else {
          close();
        }
      }}
    >
      <DialogContent aria-describedby={undefined}>
        <DialogHeader showCloseButton={false}>
          <DialogTitle>{`Remove ${shortSource(source.url)}?`}</DialogTitle>
        </DialogHeader>
        {installed.length > 0 && (
          <label htmlFor={checkboxId} className="flex items-start gap-2.5">
            <Checkbox
              id={checkboxId}
              checked={withSkills}
              onCheckedChange={(checked) => {
                setWithSkills(checked === true);
              }}
            />
            <span className="flex min-w-0 flex-col gap-0.5">
              <span className="type-body text-text-primary">
                {`Also remove its ${count(installed.length, "installed skill")}`}
              </span>
              <span className="type-technical text-text-muted">
                {installed.join(", ")}
              </span>
            </span>
          </label>
        )}
        <DialogFooter>
          <DialogClose asChild>
            <Button variant="ghost">Cancel</Button>
          </DialogClose>
          <Button variant="danger" onClick={remove}>
            Remove
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
