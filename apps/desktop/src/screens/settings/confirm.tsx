import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "#/components/ui/alert-dialog.tsx";
// The confirm dialog of the Settings sections and of removing an agent on the Agents page: a
// title, a line, an optional path, and Cancel beside one danger button that runs the command.
import { Button } from "#/components/ui/button.tsx";

/** A confirm dialog with Cancel and one button that runs the command. */
export const Confirm = ({
  open,
  onOpenChange,
  title,
  description,
  detail,
  action,
  onConfirm,
  track,
  cancelTrack,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description: string;
  /** A path, in `type.technical`. */
  detail?: string;
  action: string;
  onConfirm: () => void;
  /** The name a click on the button that runs the command is sent under. */
  track: string;
  /** The name a click on Cancel is sent under. */
  cancelTrack: string;
}) => (
  <AlertDialog open={open} onOpenChange={onOpenChange}>
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{title}</AlertDialogTitle>
        <AlertDialogDescription>{description}</AlertDialogDescription>
      </AlertDialogHeader>
      {detail !== undefined && (
        <p className="type-technical text-text-caption m-0 break-all">
          {detail}
        </p>
      )}
      <AlertDialogFooter>
        <AlertDialogCancel asChild>
          <Button variant="ghost" track={cancelTrack}>
            Cancel
          </Button>
        </AlertDialogCancel>
        <Button variant="danger" track={track} onClick={onConfirm}>
          {action}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
);
