// The telemetry question (docs/help-center/telemetry.mdx): asked once, when the active serve
// child sends its `consent` event after the first inventory of a home whose `telemetry` setting
// has no value. It lists what is sent, and both answers run `config set telemetry`, which gives
// the setting a value, so serve never asks again. "Don't send" is the default: it has the focus,
// and Escape answers with it.
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "#/components/ui/alert-dialog.tsx";
import { Button } from "#/components/ui/button.tsx";
import { openInBrowser } from "#/lib/open-url.ts";
import { useAppStore } from "#/store/store.ts";
import { TELEMETRY_PAGE, setTelemetry } from "#/store/telemetry.ts";

/** What each command sends, field by field, as the help center lists it. */
const FIELDS: [string, string][] = [
  [
    "telemetry_id",
    "an id derived from the machine id, which it cannot be traced back to",
  ],
  ["cli_version", "the version agentx version prints"],
  ["schema_version", "the version of the --json output"],
  ["os, arch", "darwin or linux, amd64 or arm64"],
  ["command", "the command words only, without arguments or flag values"],
  ["exit_code", "the exit code"],
  ["duration_ms", "how long the command ran"],
  ["json", "whether you gave --json"],
  ["at", "when the command ended, to the minute"],
];

/** Answers the question: no `consent` event asks it again this session. */
const answer = (on: boolean) => {
  useAppStore.setState({ telemetryConsent: "answered" });
  void setTelemetry(on);
};

export const TelemetryDialog = () => {
  const asking = useAppStore((s) => s.telemetryConsent === "asking");
  return (
    <AlertDialog
      open={asking}
      onOpenChange={(open) => {
        if (!open) {
          answer(false);
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Send anonymous usage data?</AlertDialogTitle>
          <AlertDialogDescription>
            Each command sends these fields and nothing else: no path, skill
            name, server name, source URL, hostname or machine label.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <ul aria-label="What is sent" className="m-0 flex flex-col gap-1 p-0">
          {FIELDS.map(([name, meaning]) => (
            <li key={name} className="type-secondary text-text-muted list-none">
              <span className="type-technical text-text-secondary">{name}</span>{" "}
              {meaning}
            </li>
          ))}
        </ul>
        <p className="type-secondary text-text-muted m-0">
          A crash sends the error type, its message and a stack trace, with
          paths removed. Change this any time in Settings.
        </p>
        <AlertDialogFooter>
          <div className="flex min-w-0 flex-1 items-center">
            <Button
              variant="link"
              size="inline"
              onClick={() => {
                void openInBrowser(TELEMETRY_PAGE);
              }}
            >
              What is sent
            </Button>
          </div>
          <AlertDialogCancel asChild>
            <Button variant="ghost">Don&apos;t send</Button>
          </AlertDialogCancel>
          <Button
            variant="primary"
            onClick={() => {
              answer(true);
            }}
          >
            Send
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
};
