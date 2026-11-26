// The Run checks dialog: Settings › Run checks runs the startup checks again and lists them here,
// over the screen it came from. Done closes it; Copy report copies doctor's output.
import { CheckList } from "#/components/check-row.tsx";
import { CopyButton } from "#/components/copy-button.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import { checksTitle, readChecks, report } from "#/startup/checks.ts";
import { closeChecks, useStartup } from "#/startup/startup.ts";

export const ChecksDialog = () => {
  const { findings, mode, open, running } = useStartup();
  const read = readChecks(findings, running);
  return (
    <Dialog
      open={open && mode === "manual"}
      onOpenChange={(next) => {
        if (!next) {
          closeChecks();
        }
      }}
    >
      <DialogContent aria-describedby={undefined}>
        <DialogHeader showCloseButton={false}>
          <DialogTitle>{checksTitle(read, running)}</DialogTitle>
        </DialogHeader>
        <div className="min-h-0 overflow-auto">
          <CheckList checks={read.checks} />
        </div>
        <DialogFooter>
          <CopyButton
            label="Copy report"
            track="checks-dialog.copy-report"
            disabled={running}
            text={() => report(findings)}
          />
          <Button
            variant="primary"
            track="checks-dialog.done"
            autoFocus
            onClick={closeChecks}
          >
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
