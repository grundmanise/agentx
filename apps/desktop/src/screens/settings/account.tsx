// The Account repo section of Settings: the Git repository your account skills publish to. With none
// set, a URL field and Save, which runs `source add <url> --account`. With one set, its URL, a
// Read-only tag when access is limited, and Change… (a new URL, after the replace warning) and Unlink (`source remove <url>`, after its warning).
// What it shows is the Sources slice, which `source list` fills again after each command; a
// failure of Save shows under the field, in the CLI's words as one sentence-cased paragraph, a
// hint that names a command put in the app's words (docs/help-center/cli/source.mdx).
import { useState } from "react";
import type { ReactNode } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import { ProblemText } from "#/components/problem-text.tsx";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "#/components/ui/alert-dialog.tsx";
import { Badge } from "#/components/ui/badge.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Input } from "#/components/ui/input.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { rewriteHint } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import { SettingsSection } from "#/screens/settings/section.tsx";
import { accountSource } from "#/screens/settings/source-rows.ts";
import { agentx, usePending } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";
import type { Problem } from "#/store/store.ts";

/** The pending label of `source add <url> --account` from this section. */
export const SAVE_ACCOUNT_LABEL = "save account repo";

/** The pending label of `source remove <url>` of the account repo. */
export const DETACH_ACCOUNT_LABEL = "detach account repo";

/** A confirmation: its title, its warning, Cancel and the button that goes ahead. */
const Confirm = ({
  open,
  onOpenChange,
  title,
  warning,
  action,
  danger = false,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  warning: ReactNode;
  action: string;
  danger?: boolean;
  onConfirm: () => void;
}) => (
  <AlertDialog open={open} onOpenChange={onOpenChange}>
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{title}</AlertDialogTitle>
        <AlertDialogDescription>{warning}</AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel asChild>
          <Button variant="ghost">Cancel</Button>
        </AlertDialogCancel>
        <Button
          variant={danger ? "danger" : "primary"}
          onClick={() => {
            onOpenChange(false);
            onConfirm();
          }}
        >
          {action}
        </Button>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
);

/** The hints of `source add --account` that name a command, in the app's words. */
const saveHintRules: HintRule[] = [
  {
    app: () => "remove it under Sources first, or choose another repository",
    cli: /^remove the source first with 'agentx source remove .+', or choose another repository$/u,
  },
];

/** What went wrong with Save, as the CLI said it; undefined when the runner shows it itself. */
const saveProblem = (
  typed: string,
  outcome: Awaited<ReturnType<typeof agentx>>
): Problem | undefined => {
  // Exit 7 is the runner's "Another agentx command is running" with its Retry, and exit 8 brings
  // the startup layer back, which says why.
  if (
    outcome.exitCode === EXIT_CODES.locked ||
    outcome.exitCode === EXIT_CODES.store
  ) {
    return undefined;
  }
  const error = outcome.events.find((e) => e.type === "error");
  const message =
    outcome.exitCode === EXIT_CODES.source
      ? `Couldn't reach ${typed}`
      : (error?.message ??
        outcome.startError ??
        `agentx source add exited with code ${outcome.exitCode ?? "none"}`);
  const hint = rewriteHint(error?.hint, saveHintRules);
  return hint === undefined ? { message } : { hint, message };
};

/**
 * The URL field and Save. With `current`, the URL of the account repo set now, Save asks first
 * whether to replace it, and Cancel closes the field.
 */
const RemoteForm = ({
  current,
  onDone,
}: {
  current?: string;
  /** The account repo was saved, or Cancel was chosen. */
  onDone?: () => void;
}) => {
  const [url, setUrl] = useState(current ?? "");
  const [problem, setProblem] = useState<Problem | undefined>();
  // The URL saved when it was refused: it keeps its case when it starts the CLI's words.
  const [refused, setRefused] = useState("");
  const [confirming, setConfirming] = useState(false);
  const saving = usePending(SAVE_ACCOUNT_LABEL);
  const typed = url.trim();
  const unchanged = typed === "" || typed === current;

  const save = async () => {
    if (SAVE_ACCOUNT_LABEL in useAppStore.getState().pending) {
      return;
    }
    setProblem(undefined);
    const outcome = await agentx(["source", "add", typed, "--account"], {
      label: SAVE_ACCOUNT_LABEL,
      ownFailure: true,
      success: "Linked account repo",
    });
    if (outcome.exitCode === EXIT_CODES.ok) {
      onDone?.();
      return;
    }
    setProblem(saveProblem(typed, outcome));
    setRefused(typed);
  };

  return (
    <>
      <form
        aria-label="Account repo URL"
        className="gap-input-label flex flex-col"
        onSubmit={(e) => {
          e.preventDefault();
          if (unchanged) {
            return;
          }
          if (current === undefined) {
            void save();
          } else {
            setConfirming(true);
          }
        }}
      >
        <div className="flex items-center gap-2">
          <Input
            kind="mono"
            aria-label="Account repo URL"
            placeholder="git@github.com:you/agentx-account.git"
            aria-invalid={problem === undefined ? undefined : true}
            value={url}
            onChange={(e) => {
              setUrl(e.target.value);
            }}
          />
          {current !== undefined && (
            <Button variant="ghost" onClick={onDone}>
              Cancel
            </Button>
          )}
          <Button
            type="submit"
            variant="primary"
            disabled={unchanged || saving}
          >
            {saving && <Spinner />}
            Save
          </Button>
        </div>
        {problem !== undefined && (
          <ProblemText problem={problem} keep={[refused]} />
        )}
      </form>
      <Confirm
        open={confirming}
        onOpenChange={setConfirming}
        title="Change your account repo?"
        warning="Skills from the previous account repo will stay but show as not published."
        action="Change"
        onConfirm={() => {
          void save();
        }}
      />
    </>
  );
};

/**
 * The account repo set now: its URL, a Read-only tag when access is limited, Change… and
 * Unlink. Like a source's card, it shows a tag only for a limit.
 */
const AccountRow = ({ url, readOnly }: { url: string; readOnly: boolean }) => {
  const [changing, setChanging] = useState(false);
  const [detaching, setDetaching] = useState(false);
  const busy = usePending(DETACH_ACCOUNT_LABEL);

  const detach = () => {
    void agentx(["source", "remove", url], {
      label: DETACH_ACCOUNT_LABEL,
      success: "The account repo is no longer set",
    });
  };

  return (
    <div className="flex flex-col gap-3">
      {/* Baseline, not center: the mono URL then sits on the middle of the pill. */}
      <div className="flex min-w-0 flex-wrap items-baseline gap-2">
        <p className="type-technical text-text-primary m-0 break-all">{url}</p>
        {readOnly && <Badge tone="neutral">Read-only</Badge>}
      </div>
      {changing ? (
        <RemoteForm
          current={url}
          onDone={() => {
            setChanging(false);
          }}
        />
      ) : (
        <div className="flex items-center gap-2">
          <Button
            variant="ghost"
            size="sm"
            disabled={busy}
            onClick={() => {
              setChanging(true);
            }}
          >
            Change…
          </Button>
          <Button
            variant="danger"
            size="sm"
            disabled={busy}
            onClick={() => {
              setDetaching(true);
            }}
          >
            {busy && <Spinner />}
            Unlink
          </Button>
        </div>
      )}
      <Confirm
        open={detaching}
        onOpenChange={setDetaching}
        title="Unlink the account repo?"
        warning={
          <>
            This will disconnect{" "}
            {/* Broken anywhere, so a long URL wraps inside the dialog. */}
            <span className="type-technical text-text-caption break-all">
              {url}
            </span>{" "}
            from the app, but keep all current skills in place.
          </>
        }
        action="Unlink"
        danger
        onConfirm={detach}
      />
    </div>
  );
};

/** The Account repo section, from the Sources slice. */
export const AccountSection = () => {
  const account = useAppStore((s) => accountSource(s.sources));
  return (
    <SettingsSection
      title="Account repo"
      description="The Git repository your skills publish to."
    >
      {account === undefined ? (
        <RemoteForm />
      ) : (
        <AccountRow
          // A new account repo starts with its field closed.
          key={account.url}
          url={account.url}
          readOnly={account.access === "read-only"}
        />
      )}
    </SettingsSection>
  );
};
