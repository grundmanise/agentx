// The Import settings section of Settings: replaces this machine's settings with the ones in a
// file Export settings wrote, a snapshot in the CLI's words (docs/help-center/cli/export.mdx).
// The user picks the file in the native open dialog and confirms, then the section runs
// `import <file> --yes`, reads `source list`, and runs `source add` for each source of the
// imported settings that this machine has not downloaded, one after another, without asking.
// The result is the names of the skills the import installed and updated, the ones it skipped
// as a warning with the CLI's reason on each name, and the sources it could not reach. The app
// writes no file: the only path it handles is the one the dialog returns, passed to the CLI.
import { useState } from "react";
import type { ReactNode } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { ImportSkillEvent, SourceEvent } from "#/cli/events.gen.ts";
import { AlertTriangleIcon } from "#/components/icons/alert-triangle.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { pickSnapshot } from "#/lib/file-dialog.ts";
import { Confirm } from "#/screens/settings/confirm.tsx";
import { SettingsSection } from "#/screens/settings/section.tsx";
import {
  fileName,
  notFetched,
  sourceAddArgs,
} from "#/screens/settings/transfer.ts";
import { loadSources } from "#/store/loaders.ts";
import { runCommand } from "#/store/runner.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";

const IMPORT_LABEL = "import settings";
const ADD_SOURCES_LABEL = "add imported sources";

/** What the last import of this visit did: its `import_skill` events and the sources it could not add. */
interface ImportResult {
  skills: ImportSkillEvent[];
  unreachable: string[];
}

/**
 * Adds each source of the imported settings this machine has not downloaded, as the settings
 * hold it, one after another, and returns the URLs of the ones that failed. A failure is the
 * result's to show, so no error toast shows for it.
 */
const addMissingSources = async (sources: readonly SourceEvent[]) => {
  const unreachable: string[] = [];
  for (const source of notFetched(sources)) {
    // One after another: each source add takes the lock, so a second would wait or fail.
    // oxlint-disable-next-line no-await-in-loop
    const outcome = await runCommand(sourceAddArgs(source), {
      label: ADD_SOURCES_LABEL,
      ownFailure: true,
    });
    if (outcome.exitCode !== EXIT_CODES.ok) {
      unreachable.push(source.url);
    }
  }
  return unreachable;
};

/** The entries the import gave `action`. */
const withAction = (
  skills: ImportSkillEvent[],
  action: ImportSkillEvent["action"]
) => skills.filter((s) => s.action === action);

/** A warning line of the result: the alert triangle, then the text, in the attention tone. */
const Warning = ({ children }: { children: ReactNode }) => (
  <p className="type-secondary text-tone-attention-fg m-0 flex items-start gap-1.5">
    <span className="flex flex-none pt-0.5">
      <AlertTriangleIcon size="xs" />
    </span>
    <span className="min-w-0 break-words">{children}</span>
  </p>
);

/**
 * The result: "Installed: …" and "Updated: …", "Skipped: …" as a warning with the CLI's reason
 * on each name, and "Couldn't reach …" for a source that could not be added. A skill the import
 * kept, placed or unplaced is not shown.
 */
const Result = ({ result }: { result: ImportResult }) => {
  const installed = withAction(result.skills, "installed");
  const updated = withAction(result.skills, "updated");
  const skipped = withAction(result.skills, "skipped");
  const empty =
    installed.length === 0 && updated.length === 0 && skipped.length === 0;
  return (
    <section
      aria-label="Import result"
      className="flex min-w-0 flex-col gap-1.5"
    >
      {installed.length > 0 && (
        <p className="type-secondary text-text-secondary m-0">
          {`Installed: ${installed.map((s) => s.name).join(", ")}`}
        </p>
      )}
      {updated.length > 0 && (
        <p className="type-secondary text-text-secondary m-0">
          {`Updated: ${updated.map((s) => s.name).join(", ")}`}
        </p>
      )}
      {empty && (
        <p className="type-secondary text-text-muted m-0">
          No skills were installed or updated.
        </p>
      )}
      {skipped.length > 0 && (
        <Warning>
          {"Skipped: "}
          {skipped.map((s, i) => (
            <span key={s.name}>
              {i > 0 && ", "}
              <span
                title={s.reason}
                className="cursor-help underline decoration-dotted"
              >
                {s.name}
              </span>
            </span>
          ))}
        </Warning>
      )}
      {result.unreachable.length > 0 && (
        <Warning>{`Couldn't reach ${result.unreachable.join(", ")}`}</Warning>
      )}
    </section>
  );
};

/** Import…: the open dialog, the confirm, `import <file> --yes`, the sources, and the result. */
const ImportFile = () => {
  const library = useAppStore((s) => s.machine?.library);
  const [picked, setPicked] = useState<string>();
  const [result, setResult] = useState<ImportResult>();
  const [adding, setAdding] = useState(false);
  const pending = useSettling(IMPORT_LABEL) || adding;

  const pick = async () => {
    const path = await pickSnapshot(library);
    if (path !== null) {
      setPicked(path);
    }
  };

  /** The import, then the sources it names that this machine lacks; undefined when it failed. */
  const runImport = async (path: string): Promise<ImportResult | undefined> => {
    const outcome = await runUntilSettled(["import", path, "--yes"], {
      label: IMPORT_LABEL,
      success: `Imported settings from ${fileName(path)}`,
    });
    if (outcome.exitCode !== EXIT_CODES.ok) {
      return undefined;
    }
    // The settings name the sources now; which of them this machine has downloaded is what
    // `source list` reports.
    await loadSources();
    const unreachable = await addMissingSources(useAppStore.getState().sources);
    return {
      skills: outcome.events.filter((e) => e.type === "import_skill"),
      unreachable,
    };
  };

  const importFile = async (path: string) => {
    setPicked(undefined);
    setResult(undefined);
    setAdding(true);
    const done = await runImport(path);
    setAdding(false);
    setResult(done);
  };

  return (
    <div className="flex flex-col gap-3">
      <span>
        <Button
          variant="secondary"
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
        title={`Replace settings with ${fileName(picked ?? "")}?`}
        description="Missing skills will be installed during import, and existing might get updated."
        detail={picked}
        action="Import"
        onConfirm={() => {
          if (picked !== undefined) {
            void importFile(picked);
          }
        }}
      />
      {result !== undefined && <Result result={result} />}
    </div>
  );
};

export const ImportSettingsSection = () => (
  <SettingsSection
    title="Import settings"
    description="Replaces the settings on this machine with the ones from an exported file."
  >
    <ImportFile />
  </SettingsSection>
);
