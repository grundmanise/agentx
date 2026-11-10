// Adopt all, above the Skills list: one line per tool whose skills the adoption preview offers to
// adopt, "{n} skills added by {tool}" with Adopt all, which runs `adopt <tool> --all` and reads
// "Adopting {current} of {total}", in skills, from the run's progress. One run at a time. The
// toast and the activity entry count the skills the run adopted and the ones it could not, of
// those the line offered; a skill it could
// not adopt stays a candidate, with its banner, and its DETAILS give the CLI's reason. The line
// goes once the preview that follows the run offers none of the tool's skills.
import { useId } from "react";
import { create } from "zustand";

import type { AdoptionEvent } from "#/cli/events.gen.ts";
import { EXIT_CODES } from "#/cli/events.gen.ts";
import { Spinner } from "#/components/spinner.tsx";
import { notify, notifyAttention, notifyError } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import { plural } from "#/lib/plural.ts";
import { toolName } from "#/screens/skills/projection.ts";
import { loadAdoption } from "#/store/loaders.ts";
import { failureToast } from "#/store/runner.ts";
import type { ActivityText, Outcome } from "#/store/runner.ts";
import { afterSettled, runUntilSettled } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";

/** A tool `adopt` takes the skills of: the `tool` of its records, and the argument that names it. */
interface Tool {
  tool: AdoptionEvent["tool"];
  arg: "skills-cli" | "skillshare";
}

/** The tools, in the order of their lines. */
const TOOLS: Tool[] = [
  { arg: "skills-cli", tool: "vercel" },
  { arg: "skillshare", tool: "skillshare" },
];

interface Run {
  /** The tool whose skills the run adopts. */
  arg: Tool["arg"];
  /** How many skills the line offered when the run started. */
  total: number;
  /** How many skills the run has started to install: its `blobs` progress events so far. */
  started: number;
}

/** The Adopt all run, from the click until the preview that follows it: one at a time. */
const useRun = create<{ run: Run | undefined }>()(() => ({ run: undefined }));

/** The preview's candidates of `tool`: what Adopt all would take over. */
const candidatesOf = (adoption: readonly AdoptionEvent[], tool: Tool) =>
  adoption.filter((r) => r.tool === tool.tool && r.state === "candidate");

/**
 * "Adopting 2 of 3", in skills: each skill the run installs ends one `blobs` step first, while a
 * `fetch` is a source and the `rescan` ends the run. "Adopting…" before the first skill.
 */
const progressText = ({ started, total }: Run) =>
  started === 0
    ? "Adopting…"
    : `Adopting ${Math.min(started, total)} of ${total}`;

/**
 * What a run that did not exit 0 but reported its skills says, toast and activity entry alike:
 * how many it adopted and how many of the line's it could not. Undefined for a run that reported
 * none, which the runner words.
 */
const wordingOf = (
  { events, exitCode }: Outcome,
  offered: Set<string>
): ActivityText | undefined => {
  const adoption = events.filter((e) => e.type === "adoption");
  if (exitCode === EXIT_CODES.ok || adoption.length === 0) {
    return undefined;
  }
  const adopted = adoption.filter((e) => e.state === "adopted").length;
  // Only the skills the line offered: a record the preview already refused is no news.
  const refused = adoption.filter(
    (e) => e.state === "refused" && offered.has(e.name)
  ).length;
  if (adopted === 0) {
    return { message: "No skills could be adopted", tone: "attention" };
  }
  const message = `Adopted ${plural(adopted, "skill")}`;
  return refused === 0
    ? { message, tone: "success" }
    : {
        message: `${message}. ${refused} could not be adopted`,
        tone: "attention",
      };
};

/**
 * Shows a run that did not exit 0: its own wording when it reported its skills, else the runner's
 * error toast. A busy lock and a broken account repo the runner answers itself.
 */
const reportFailure = (
  args: readonly string[],
  outcome: Outcome,
  offered: Set<string>
) => {
  const { exitCode } = outcome;
  if (
    exitCode === EXIT_CODES.ok ||
    exitCode === EXIT_CODES.locked ||
    exitCode === EXIT_CODES.account_repo ||
    outcome.cancelled
  ) {
    return;
  }
  const wording = wordingOf(outcome, offered);
  if (wording === undefined) {
    notifyError(failureToast(args, outcome));
  } else if (wording.tone === "success") {
    notify(wording.message);
  } else {
    notifyAttention({ message: wording.message, warnings: [] });
  }
};

/**
 * Runs `adopt <tool> --all`, and holds the lines pending until the preview and the inventory that
 * follow it have both arrived. Neither the run nor the read rejects.
 */
const adoptAll = async (tool: Tool, offered: Set<string>) => {
  useRun.setState({ run: { arg: tool.arg, started: 0, total: offered.size } });
  const args = ["adopt", tool.arg, "--all"];
  const outcome = await runUntilSettled(args, {
    label: `adopt all ${tool.arg}`,
    onProgress: (progress) => {
      if (progress.phase === "blobs") {
        useRun.setState(({ run }) => ({
          run: run === undefined ? run : { ...run, started: run.started + 1 },
        }));
      }
    },
    ownActivity: (failed) => wordingOf(failed, offered),
    ownFailure: true,
    success: (events) =>
      `Adopted ${plural(
        events.filter((e) => e.type === "adoption" && e.state === "adopted")
          .length,
        "skill"
      )}`,
  });
  reportFailure(args, outcome, offered);
  await loadAdoption();
  afterSettled(() => {
    useRun.setState({ run: undefined });
  });
};

/** One tool's line: the count of its skills to adopt, and Adopt all. */
const AdoptAllLine = ({
  tool,
  candidates,
  run,
}: {
  tool: Tool;
  candidates: AdoptionEvent[];
  run: Run | undefined;
}) => {
  const id = useId();
  const [first] = candidates;
  if (first === undefined) {
    return null;
  }
  const text = `${plural(candidates.length, "skill")} added by ${toolName(first)}`;
  const mine = run?.arg === tool.arg;
  return (
    <fieldset
      aria-labelledby={id}
      className="h-split-adopt m-0 flex min-w-0 items-center justify-between gap-3 border-0 p-0"
    >
      <span id={id} className="type-secondary text-text-secondary truncate">
        {text}
      </span>
      <Button
        variant="ghost"
        size="sm"
        disabled={run !== undefined}
        aria-busy={mine ? true : undefined}
        onClick={() => {
          void adoptAll(tool, new Set(candidates.map((c) => c.name)));
        }}
      >
        {mine && <Spinner />}
        {mine ? progressText(run) : "Adopt all"}
      </Button>
    </fieldset>
  );
};

/** The Adopt all lines above the Skills list; nothing while the preview offers no skill. */
export const AdoptAllLines = () => {
  const adoption = useAppStore((s) => s.adoption);
  const run = useRun((s) => s.run);
  const lines = TOOLS.map((tool) => ({
    candidates: candidatesOf(adoption, tool),
    tool,
  })).filter((line) => line.candidates.length > 0);
  if (lines.length === 0) {
    return null;
  }
  return (
    <div className="pr-split-adopt pb-split-adopt-bottom pl-split-adopt-left flex flex-col pt-0">
      {lines.map(({ tool, candidates }) => (
        <AdoptAllLine
          key={tool.arg}
          tool={tool}
          candidates={candidates}
          run={run}
        />
      ))}
    </div>
  );
};
