// The Remove dialog of a shared source on the Sources screen: Remove runs `source remove <url>`,
// with `--skills` when the user also chose the skills this machine installed from it. There is no
// Undo: the skills removed are gone, and the source is added again with Add source.
import { useId, useRef, useState } from "react";

import type { LibraryEntry, Source } from "#/cli/events.gen.ts";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "#/components/ui/alert-dialog.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Checkbox } from "#/components/ui/checkbox.tsx";
import { shortSource } from "#/screens/skills/projection.ts";
import {
  count,
  installedFrom,
  removedText,
} from "#/screens/sources/projection.ts";
import { agentx } from "#/store/runner.ts";

/** The pending label of the removal of the source `id`, with its skills when chosen. */
export const removeSourceLabel = (id: string) => `remove source ${id}`;

/** Removes the source, with its `skills` installed skills when there are any; the toast names what went. */
const removeSource = (source: Source, skills: number) => {
  const args = ["source", "remove", source.url];
  if (skills > 0) {
    args.push("--skills");
  }
  void agentx(args, {
    label: removeSourceLabel(source.id),
    success: removedText(skills),
  });
};

export const RemoveSourceDialog = ({
  source,
  library,
  open,
  onOpenChange,
  onCloseAutoFocus,
}: {
  source: Source;
  library: LibraryEntry[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Moves focus when the dialog closes, which has no trigger to return to; `confirmed` is true after Remove. */
  onCloseAutoFocus: (event: Event, confirmed: boolean) => void;
}) => {
  const [withSkills, setWithSkills] = useState(false);
  const checkboxId = useId();
  // Whether this close is the confirmation: its Remove button may unmount before focus moves.
  const confirmed = useRef(false);
  const installed = installedFrom(source.url, library);
  // The choice lasts one confirmation: a dialog opened again starts unticked.
  const close = () => {
    setWithSkills(false);
    onOpenChange(false);
  };
  const remove = () => {
    confirmed.current = true;
    removeSource(source, withSkills ? installed.length : 0);
    close();
  };
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => {
        if (next) {
          onOpenChange(true);
        } else {
          close();
        }
      }}
    >
      <AlertDialogContent
        aria-describedby={undefined}
        onCloseAutoFocus={(event) => {
          onCloseAutoFocus(event, confirmed.current);
          confirmed.current = false;
        }}
      >
        <AlertDialogHeader>
          <AlertDialogTitle>{`Remove ${shortSource(source.url)}?`}</AlertDialogTitle>
        </AlertDialogHeader>
        {installed.length > 0 && (
          <label htmlFor={checkboxId} className="flex items-start gap-2.5">
            <Checkbox
              id={checkboxId}
              track="remove-source-dialog.with-skills"
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
        <AlertDialogFooter>
          <AlertDialogCancel asChild>
            <Button variant="ghost" track="remove-source-dialog.cancel">
              Cancel
            </Button>
          </AlertDialogCancel>
          <Button
            variant="danger"
            track="remove-source-dialog.remove"
            onClick={remove}
          >
            Remove
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
};
