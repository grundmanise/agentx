// The confirm dialog of the Settings sections and of removing an agent on the Agents page: a
// title, a line, an optional path, and Cancel beside one danger button that runs the command.
import { Button } from "#/components/ui/button.tsx";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "#/components/ui/dialog.tsx";

/** A confirm dialog with Cancel and one button that runs the command. */
export const Confirm = ({
  open,
  onOpenChange,
  title,
  description,
  detail,
  action,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  /** A path, in `type.technical`. */
  detail?: string;
  action: string;
  onConfirm: () => void;
}) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent>
      <DialogHeader showCloseButton={false}>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </DialogHeader>
      {detail !== undefined && (
        <p className="type-technical text-text-caption m-0 break-all">
          {detail}
        </p>
      )}
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="ghost">Cancel</Button>
        </DialogClose>
        <Button variant="danger" onClick={onConfirm}>
          {action}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
);
