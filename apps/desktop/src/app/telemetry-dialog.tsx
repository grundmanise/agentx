// The telemetry question (docs/help-center/telemetry.mdx): asked once the app has started, in a
// release build (one whose CLI reports a stable or nightly version), while the `settings` event
// says the `telemetry` setting has no value. It lists what may be sent, and both answers run
// `config set telemetry`, which gives the setting a value, so no later launch asks. "Don't allow"
// is the default: it has the focus, and Escape answers with it.
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
import { releaseChannel } from "#/lib/telemetry.ts";
import { useStartup } from "#/startup/startup.ts";
import { useAppStore } from "#/store/store.ts";
import { TELEMETRY_PAGE, setTelemetry } from "#/store/telemetry.ts";

/** What may be sent, as the help center lists it. */
const SENT = [
  "the commands the app runs, as in the terminal",
  "the screens you open and the buttons you click, by name only",
  "errors and crashes, with every path removed",
  "the app version, release channel, OS, screen size, language and time zone",
  "a non-reversible, hashed anonymous id",
];

/** Answers the question, which is not asked again this session, even if the command fails. */
const answer = (on: boolean) => {
  useAppStore.setState({ telemetryAnswered: true });
  void setTelemetry(on);
};

export const TelemetryDialog = () => {
  const release = useStartup((s) => {
    const { version } = s.findings;
    return (
      version !== undefined &&
      "cliVersion" in version &&
      releaseChannel(version.cliVersion) !== undefined
    );
  });
  const unanswered = useAppStore(
    (s) =>
      !s.telemetryAnswered &&
      s.settings !== undefined &&
      s.settings.telemetry === undefined
  );
  return (
    <AlertDialog
      open={release && unanswered}
      onOpenChange={(open) => {
        if (!open) {
          answer(false);
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Help improve agentx</AlertDialogTitle>
          <AlertDialogDescription>
            Send anonymous usage data to PostHog? We use it to find what breaks
            and to make agentx better for you.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <div className="flex flex-col gap-1">
          <p className="type-secondary text-text-secondary m-0">It may send:</p>
          <ul
            aria-label="What may be sent"
            className="m-0 flex flex-col gap-1 pl-5"
          >
            {SENT.map((line) => (
              <li
                key={line}
                className="type-secondary text-text-muted list-disc"
              >
                {line}
              </li>
            ))}
          </ul>
        </div>
        <p className="type-secondary text-text-muted m-0">
          It never sends paths, skill or server names, URLs, on-screen text or
          anything you type. Your IP address is not stored.
        </p>
        <AlertDialogFooter>
          <div className="flex min-w-0 flex-1 items-center">
            <Button
              track="telemetry-dialog.details"
              variant="link"
              size="inline"
              onClick={() => {
                void openInBrowser(TELEMETRY_PAGE);
              }}
            >
              Details
            </Button>
          </div>
          <AlertDialogCancel asChild>
            <Button track="telemetry-dialog.dont-allow" variant="ghost">
              Don&apos;t allow
            </Button>
          </AlertDialogCancel>
          <Button
            track="telemetry-dialog.allow"
            variant="primary"
            onClick={() => {
              answer(true);
            }}
          >
            Allow anonymous data
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
};
