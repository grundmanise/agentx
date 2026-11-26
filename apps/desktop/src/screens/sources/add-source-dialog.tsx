import { useState } from "react";

import { Button } from "#/components/ui/button.tsx";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import { Input } from "#/components/ui/input.tsx";

/** The field, its hint and the buttons. Mounted while the dialog is open. */
const AddSourceForm = ({ onAdd }: { onAdd: (typed: string) => void }) => {
  const [url, setUrl] = useState("");
  return (
    <form
      className="gap-dialog flex flex-col"
      onSubmit={(e) => {
        e.preventDefault();
        const typed = url.trim();
        if (typed !== "") {
          onAdd(typed);
        }
      }}
    >
      <div className="gap-input-label flex flex-col">
        <Input
          autoFocus
          kind="mono"
          aria-label="Git URL or owner/name"
          placeholder="Git URL or owner/name"
          value={url}
          onChange={(e) => {
            setUrl(e.target.value);
          }}
        />
        <p className="type-secondary text-text-hint m-0">
          Add #branch or #tag to pin it
        </p>
      </div>
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="ghost" track="add-source-dialog.cancel">
            Cancel
          </Button>
        </DialogClose>
        <Button
          type="submit"
          variant="primary"
          track="add-source-dialog.add"
          disabled={url.trim() === ""}
        >
          Add
        </Button>
      </DialogFooter>
    </form>
  );
};

/**
 * The Add source dialog: a URL or `owner/name`, with `#<ref>` to pin it, for `source add`
 * (docs/help-center/cli/source.mdx). Add closes it at once and hands the link to `onAdd`, which
 * runs the add while the user goes on (add-source.ts). The form is mounted while the dialog is
 * open, so it opens clean each time.
 */
export const AddSourceDialog = ({
  open,
  onOpenChange,
  onAdd,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onAdd: (typed: string) => void;
}) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent aria-describedby={undefined}>
      <DialogHeader>
        <DialogTitle>Add source</DialogTitle>
      </DialogHeader>
      <AddSourceForm
        onAdd={(typed) => {
          onOpenChange(false);
          onAdd(typed);
        }}
      />
    </DialogContent>
  </Dialog>
);
