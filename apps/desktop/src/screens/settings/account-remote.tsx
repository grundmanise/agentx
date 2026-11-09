// The Account remote section of Settings: the Git repository your own skills publish to. With none
// set, a URL field and Save, which runs `source add <url> --account`. With one set, its URL, how
// many of your skills it holds and when it was fetched, whether you can write to it, and Change…
// (a new URL, after the replace warning) and Detach (`source remove <url>`, after its warning).
// What it shows is the Sources slice, which `source list` fills again after each command; a
// failure of Save shows under the field, in the CLI's words as one sentence-cased paragraph
// (docs/help-center/cli/source.mdx).
import { useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import { ProblemText } from "#/components/problem-text.tsx";
import { Spinner } from "#/components/spinner.tsx";
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
import { Input } from "#/components/ui/input.tsx";
import { useNow } from "#/screens/settings/clock.ts";
import { SettingsSection } from "#/screens/settings/section.tsx";
import {
  accessLine,
  accountLine,
  accountRemote,
} from "#/screens/settings/source-rows.ts";
import { runCommand, usePending } from "#/store/runner.ts";
import { useAppStore } from "#/store/store.ts";
import type { Problem } from "#/store/store.ts";

/** The pending label of `source add <url> --account` from this section. */
export const SAVE_ACCOUNT_LABEL = "save account remote";

/** The pending label of `source remove <url>` of the account remote. */
export const DETACH_ACCOUNT_LABEL = "detach account remote";

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
  warning: string;
  action: string;
  danger?: boolean;
  onConfirm: () => void;
}) => (
  <Dialog open={open} onOpenChange={onOpenChange}>
    <DialogContent>
      <DialogHeader showCloseButton={false}>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{warning}</DialogDescription>
      </DialogHeader>
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="ghost">Cancel</Button>
        </DialogClose>
        <Button
          variant={danger ? "danger" : "primary"}
          onClick={() => {
            onOpenChange(false);
            onConfirm();
          }}
        >
          {action}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
);

/** What went wrong with Save, as the CLI said it; undefined when the runner shows it itself. */
const saveProblem = (
  typed: string,
  outcome: Awaited<ReturnType<typeof runCommand>>
): Problem | undefined => {
  // Exit 7 is the runner's "Another agentx command is running" with its Retry, and exit 8 brings
  // the startup layer back, which says why.
  if (
    outcome.exitCode === EXIT_CODES.locked ||
    outcome.exitCode === EXIT_CODES.account_repo
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
  return error?.hint === undefined
    ? { message }
    : { hint: error.hint, message };
};

/**
 * The URL field and Save. With `current`, the URL of the account remote set now, Save asks first
 * whether to replace it, and Cancel closes the field.
 */
const RemoteForm = ({
  current,
  onDone,
}: {
  current?: string;
  /** The account remote was saved, or Cancel was chosen. */
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
    const outcome = await runCommand(["source", "add", typed, "--account"], {
      label: SAVE_ACCOUNT_LABEL,
      ownFailure: true,
      success: (events) => {
        const source = events.find((e) => e.type === "source");
        return `Saved. Reached ${source?.url ?? typed}`;
      },
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
        aria-label="Account remote URL"
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
            aria-label="Account remote URL"
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
        title="Replace the account remote?"
        warning="Your skills stay on this machine and show as not published until you publish each one to the new account remote."
        action="Replace"
        onConfirm={() => {
          void save();
        }}
      />
    </>
  );
};

/** The account remote set now: its URL, its skills and fetch, its access, Change… and Detach. */
const AccountRemoteRow = ({
  url,
  line,
  access,
}: {
  url: string;
  line: string;
  access: string;
}) => {
  const [changing, setChanging] = useState(false);
  const [detaching, setDetaching] = useState(false);
  const busy = usePending(DETACH_ACCOUNT_LABEL);

  const detach = () => {
    void runCommand(["source", "remove", url], {
      label: DETACH_ACCOUNT_LABEL,
      success: "The account remote is no longer set",
    });
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-col gap-1">
        <p className="type-technical text-text-primary m-0 break-all">{url}</p>
        <p className="type-secondary text-text-muted m-0">{line}</p>
        <p className="type-secondary text-text-hint m-0">{access}</p>
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
            Detach
          </Button>
        </div>
      )}
      <Confirm
        open={detaching}
        onOpenChange={setDetaching}
        title={`Detach ${url}?`}
        warning="Your skills stay on this machine, and nothing on the account remote changes."
        action="Detach"
        danger
        onConfirm={detach}
      />
    </div>
  );
};

/** The Account remote section, from the Sources slice. */
export const AccountRemoteSection = () => {
  const account = useAppStore((s) => accountRemote(s.sources));
  const now = useNow();
  return (
    <SettingsSection
      title="Account remote"
      description="The Git repository your own skills publish to."
    >
      {account === undefined ? (
        <RemoteForm />
      ) : (
        <AccountRemoteRow
          // A new account remote starts with its field closed.
          key={account.url}
          url={account.url}
          line={accountLine(account, now)}
          access={accessLine(account.access)}
        />
      )}
    </SettingsSection>
  );
};
