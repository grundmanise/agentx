// The Another machine section of Settings: export this machine's settings and the list of its
// skills to a snapshot in the library, and import one onto this machine
// (docs/help-center/cli/export.mdx). Export runs `export <name>`, and `export <name> --force`
// once the user confirms an overwrite. Import lets the user pick the file in the native open
// dialog, confirms, then runs `import <file> --yes`, lists the `import_skill` events, and offers
// to add the sources of the imported settings that this machine has not fetched, one
// `source add` after another. The app writes no file: the only path it handles is the one the
// dialog returns, passed to the CLI.
import { useId, useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  ImportSkillEvent,
  ImportState,
  SourceEvent,
} from "#/cli/events.gen.ts";
import { Pill } from "#/components/pill.tsx";
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
import { pickSnapshot } from "#/lib/file-dialog.ts";
import { plural } from "#/lib/plural.ts";
import {
  DEFAULT_SNAPSHOT_NAME,
  fileName,
  isSnapshotName,
  notFetched,
  sourceAddArgs,
} from "#/screens/settings/transfer.ts";
import { loadSources } from "#/store/loaders.ts";
import { runCommand, usePending } from "#/store/runner.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";
import type { Problem } from "#/store/store.ts";

/** The section's label and the line under it. */
export const anotherMachine = {
  description:
    "Your settings and the list of your skills, so another machine can bring them back. No skill content.",
  label: "Another machine",
};

const EXPORT_LABEL = "export settings";
const IMPORT_LABEL = "import settings";
const ADD_SOURCES_LABEL = "add imported sources";

/** The CLI's message and hint, under the field it is about. */
const ProblemLines = ({ problem }: { problem: Problem }) => (
  <div role="alert" className="flex flex-col gap-1">
    <p className="type-notice text-tone-danger-fg m-0">{problem.message}</p>
    {problem.hint !== undefined && (
      <p className="type-notice text-text-muted m-0">{problem.hint}</p>
    )}
  </div>
);

/** A confirm dialog with Cancel and one button that runs the command. */
const Confirm = ({
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

/** Export: the snapshot's name, and Export settings, which writes `<name>.json` to the library. */
const ExportRow = () => {
  const [name, setName] = useState(DEFAULT_SNAPSHOT_NAME);
  const [problem, setProblem] = useState<Problem | undefined>();
  const [overwrite, setOverwrite] = useState(false);
  const pending = usePending(EXPORT_LABEL);
  const hintId = useId();
  const valid = isSnapshotName(name);

  const exportAs = async (force: boolean) => {
    setProblem(undefined);
    const args = force ? ["export", name, "--force"] : ["export", name];
    const outcome = await runCommand(args, {
      label: EXPORT_LABEL,
      ownFailure: true,
      success: (events) => {
        const written = events.find((e) => e.type === "export");
        return `Exported to ${written?.path ?? `${name}.json`}`;
      },
    });
    if (outcome.exitCode === EXIT_CODES.ok) {
      return;
    }
    // A file of that name is already in the library: replacing it is the user's call.
    if (outcome.exitCode === EXIT_CODES.refused && !force) {
      setOverwrite(true);
      return;
    }
    const error = outcome.events.find((e) => e.type === "error");
    setProblem(
      error === undefined
        ? {
            message:
              outcome.startError ??
              `agentx export exited with code ${outcome.exitCode ?? "none"}`,
          }
        : { hint: error.hint, message: error.message }
    );
  };

  return (
    <form
      className="flex flex-col gap-2"
      aria-label="Export settings"
      onSubmit={(e) => {
        e.preventDefault();
        if (valid && !pending) {
          void exportAs(false);
        }
      }}
    >
      <div className="flex items-center gap-2">
        <Input
          kind="mono"
          aria-label="Snapshot name"
          aria-describedby={hintId}
          aria-invalid={valid && problem === undefined ? undefined : true}
          value={name}
          onChange={(e) => {
            setName(e.target.value);
            setProblem(undefined);
          }}
        />
        <Button type="submit" variant="secondary" disabled={!valid || pending}>
          {pending && <Spinner />}
          Export settings
        </Button>
      </div>
      <p id={hintId} className="type-secondary text-text-muted m-0">
        {valid
          ? `Written to your library as ${name}.json`
          : "Give a file name without an extension or a slash"}
      </p>
      {problem !== undefined && <ProblemLines problem={problem} />}
      <Confirm
        open={overwrite}
        onOpenChange={setOverwrite}
        title={`Replace ${name}.json?`}
        description="Your library already holds a snapshot of this name. Export replaces it with this machine's settings and skills as they are now."
        action="Replace"
        onConfirm={() => {
          setOverwrite(false);
          void exportAs(true);
        }}
      />
    </form>
  );
};

/** What this machine held of the entry before the import. */
const before: Record<ImportState, string> = {
  different: "Another version was here",
  missing: "New to this machine",
  present: "Already here",
};

/** Where the entry's skill comes from, as the snapshot names it. */
const origin = (entry: ImportSkillEvent) => {
  if (entry.source !== undefined) {
    return entry.subpath === undefined || entry.subpath === ""
      ? entry.source
      : `${entry.source}/${entry.subpath}`;
  }
  return entry.kind === "unmanaged" ? "No source" : "Your account remote";
};

/** One line of the import result: the skill, what import did with it, its source, and why when it was skipped. */
const ImportedSkill = ({ entry }: { entry: ImportSkillEvent }) => (
  <li className="border-shell-divider flex min-w-0 flex-col gap-1 border-b py-2.5">
    <span className="flex min-w-0 items-center gap-2">
      <span className="type-row-name text-text-primary truncate">
        {entry.name}
      </span>
      <Pill tone="neutral">{entry.action}</Pill>
    </span>
    <span className="type-technical text-text-caption truncate">
      {`${origin(entry)} · ${before[entry.state]}`}
    </span>
    {entry.reason !== undefined && entry.reason !== "" && (
      <span className="type-secondary text-text-muted">{entry.reason}</span>
    )}
  </li>
);

/** A source to add again: its URL as `source add` takes it. */
const sourceLine = (source: SourceEvent) =>
  source.account === true
    ? `${source.url} (account remote)`
    : (sourceAddArgs(source)[2] ?? source.url);

/**
 * The sources of the imported settings this machine has not fetched, and Add sources, which runs
 * `source add` for each, one after another, as the settings hold it.
 */
const SourcesToAdd = () => {
  const sources = useAppStore((s) => s.sources);
  const missing = notFetched(sources);
  const [step, setStep] = useState<{ current: number; total: number }>();
  if (missing.length === 0) {
    return null;
  }
  const addAll = async () => {
    const queue = [...missing];
    for (const [i, source] of queue.entries()) {
      setStep({ current: i + 1, total: queue.length });
      // One after another: each source add takes the lock, so a second would wait or fail.
      // oxlint-disable-next-line no-await-in-loop
      await runUntilSettled(sourceAddArgs(source), {
        label: ADD_SOURCES_LABEL,
        success: `Added ${source.url}`,
      });
    }
    setStep(undefined);
  };
  return (
    <div className="flex flex-col gap-2.5">
      <p className="type-secondary text-text-muted m-0">
        {`The imported settings name ${plural(missing.length, "source")} this machine has not fetched. Add them before you install from them.`}
      </p>
      <ul
        aria-label="Sources to add"
        className="m-0 flex list-none flex-col gap-1 p-0"
      >
        {missing.map((s) => (
          <li
            key={s.id}
            className="type-technical text-text-secondary break-all"
          >
            {sourceLine(s)}
          </li>
        ))}
      </ul>
      <span>
        <Button
          variant="secondary"
          disabled={step !== undefined}
          aria-busy={step === undefined ? undefined : true}
          onClick={() => {
            void addAll();
          }}
        >
          {step !== undefined && <Spinner />}
          {step === undefined
            ? "Add sources"
            : `Adding ${step.current} of ${step.total}`}
        </Button>
      </span>
    </div>
  );
};

/** What the last import of this visit did: the file and its `import_skill` events. */
interface ImportResult {
  file: string;
  skills: ImportSkillEvent[];
}

/** Import…: the open dialog, the confirm, `import <file> --yes`, and its result. */
const ImportRow = () => {
  const library = useAppStore((s) => s.machine?.library);
  const [picked, setPicked] = useState<string>();
  const [result, setResult] = useState<ImportResult>();
  const pending = useSettling(IMPORT_LABEL);

  const pick = async () => {
    const path = await pickSnapshot(library);
    if (path !== null) {
      setPicked(path);
    }
  };

  const importFile = async (path: string) => {
    setPicked(undefined);
    setResult(undefined);
    const name = fileName(path);
    const outcome = await runUntilSettled(["import", path, "--yes"], {
      label: IMPORT_LABEL,
      success: `Imported the settings in ${name}`,
    });
    if (outcome.exitCode !== EXIT_CODES.ok) {
      return;
    }
    // The settings name the sources now; which of them this machine has fetched is what
    // `source list` reports.
    await loadSources();
    setResult({
      file: name,
      skills: outcome.events.filter((e) => e.type === "import_skill"),
    });
  };

  return (
    <div className="flex flex-col gap-3">
      <span>
        <Button
          variant="ghost"
          disabled={pending}
          aria-busy={pending ? true : undefined}
          onClick={() => {
            void pick();
          }}
        >
          {pending && <Spinner />}
          Import…
        </Button>
      </span>
      <Confirm
        open={picked !== undefined}
        onOpenChange={(open) => {
          if (!open) {
            setPicked(undefined);
          }
        }}
        title={`Replace this machine's settings with the ones in ${fileName(picked ?? "")}?`}
        description="Import also installs the skills it lists, moves forward the ones this machine holds, and places each one as the snapshot records. It removes no skill."
        detail={picked}
        action="Import"
        onConfirm={() => {
          if (picked !== undefined) {
            void importFile(picked);
          }
        }}
      />
      {result !== undefined && (
        <section
          aria-label="Import result"
          className="flex min-w-0 flex-col gap-3"
        >
          <p className="type-secondary text-text-muted m-0">
            {result.skills.length === 0
              ? `${result.file} lists no skills`
              : `${plural(result.skills.length, "skill")} in ${result.file}`}
          </p>
          {result.skills.length > 0 && (
            <ul
              aria-label="Imported skills"
              className="m-0 flex list-none flex-col p-0"
            >
              {result.skills.map((entry) => (
                <ImportedSkill key={entry.name} entry={entry} />
              ))}
            </ul>
          )}
          <SourcesToAdd />
        </section>
      )}
    </div>
  );
};

/** The Another machine section's content: Export, then Import. */
export const AnotherMachine = () => (
  <div className="flex min-w-0 flex-col gap-5">
    <ExportRow />
    <ImportRow />
  </div>
);
