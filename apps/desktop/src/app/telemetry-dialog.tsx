// The telemetry question (docs/help-center/telemetry.mdx): asked once the app has started, in a
// release build (one whose CLI reports a stable or nightly version), while the inventory this
// launch's serve child sent says the `telemetry` setting has no value. It links to what is sent,
// and both answers run `config set telemetry`, which gives the setting a value, so no later launch
// asks. "No thanks" is the default: it has the focus, and Escape answers with it.
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
import {
  TELEMETRY_PAGE,
  liveSettings,
  setTelemetry,
} from "#/store/telemetry.ts";

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
  const unanswered = useAppStore((s) => {
    const settings = liveSettings(s);
    return (
      !s.telemetryAnswered &&
      settings !== undefined &&
      settings.telemetry === undefined
    );
  });
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
          <AlertDialogTitle>Analytics and crash reporting</AlertDialogTitle>
          <AlertDialogDescription>
            Allow agentx to send anonymous usage data and crash reports to
            PostHog? This helps us fix bugs and improve agentx for you. They
            never include paths, names or anything you type. You can change this
            any time in Settings.
          </AlertDialogDescription>
        </AlertDialogHeader>
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
              What is sent
            </Button>
          </div>
          <AlertDialogCancel asChild>
            <Button track="telemetry-dialog.dont-allow" variant="ghost">
              No thanks
            </Button>
          </AlertDialogCancel>
          <Button
            track="telemetry-dialog.allow"
            variant="primary"
            onClick={() => {
              answer(true);
            }}
          >
            Allow
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
};
