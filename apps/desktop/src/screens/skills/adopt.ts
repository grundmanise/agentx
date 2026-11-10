// Adopting the skills another tool installed, all at once: which tools have skills to adopt, and
// the one run that adopts them, which the Adopt skills section of Settings and the Unmanaged
// group's Adopt all both start through the adopt dialog (`adopt-dialog.tsx`). The run is
// `adopt <tool> --all` per tool, one after another. Each skill of the dialog is a row whose state
// follows the run's `progress` events, and whose result is the `adoption` event the run reports
// for it. The dialog shows the result, so no toast does; the activity list still logs each run.
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { AdoptionEvent, CliEvent } from "#/cli/events.gen.ts";
import { againRule, doctorRule, rewriteHint } from "#/lib/app-hint.ts";
import { plural } from "#/lib/plural.ts";
import { toolName } from "#/screens/skills/projection.ts";
import { loadAdoption } from "#/store/loaders.ts";
import type { ActivityText, Outcome } from "#/store/runner.ts";
import { runUntilSettled } from "#/store/settle.ts";
import { addActivity } from "#/store/store.ts";
import type { Problem } from "#/store/store.ts";

/** The argument of `adopt` that names a tool. */
export type ToolArg = "skills-cli" | "skillshare";

/** A tool `adopt` takes the skills of: the `tool` of its records, and the argument that names it. */
interface Tool {
  tool: AdoptionEvent["tool"];
  arg: ToolArg;
}

/** The tools, in the order the app lists them. */
const TOOLS: Tool[] = [
  { arg: "skills-cli", tool: "vercel" },
  { arg: "skillshare", tool: "skillshare" },
];

/** One tool's records in the adoption preview: the skills to adopt and the ones it refuses. */
export interface ToolRecords {
  arg: ToolArg;
  /** The tool as the product calls it: "Skills CLI", "Skillshare". */
  name: string;
  candidates: AdoptionEvent[];
  refused: AdoptionEvent[];
}

/** The tools with a skill to adopt, from the adoption preview, in the app's order. */
export const toolsToAdopt = (adoption: readonly AdoptionEvent[]) =>
  TOOLS.flatMap((tool): ToolRecords[] => {
    const records = adoption.filter((r) => r.tool === tool.tool);
    const candidates = records.filter((r) => r.state === "candidate");
    const [first] = candidates;
    return first === undefined
      ? []
      : [
          {
            arg: tool.arg,
            candidates,
            name: toolName(first),
            refused: records.filter((r) => r.state === "refused"),
          },
        ];
  });

/**
 * Where a skill of the dialog is. Before the run every skill to adopt is `waiting`; a step of the
 * run that names it makes it `running`, and its `install` step `installed`. When the run ends its
 * `adoption` event makes it `adopted` or `failed`. A record the preview refuses is `left-out` from
 * the start, with the preview's reason.
 */
export type RowStatus =
  | "waiting"
  | "running"
  | "installed"
  | "adopted"
  | "failed"
  | "left-out";

export interface AdoptRow {
  name: string;
  tool: ToolArg;
  status: RowStatus;
  /** Why the CLI did not adopt it, in its words. */
  reason?: string;
}

/** The rows of a dialog over `tools`: each tool's skills to adopt, then the ones it refuses. */
export const rowsOf = (tools: readonly ToolRecords[]): AdoptRow[] =>
  tools.flatMap((t) => [
    ...t.candidates.map((r): AdoptRow => ({
      name: r.name,
      status: "waiting",
      tool: t.arg,
    })),
    ...t.refused.map((r): AdoptRow => ({
      name: r.name,
      status: "left-out",
      tool: t.arg,
      ...(r.reason === undefined ? {} : { reason: r.reason }),
    })),
  ]);

/** A tool a dialog covers, as it was when the dialog opened: its name and its counts. */
export interface ToolSummary {
  arg: ToolArg;
  name: string;
  /** Its skills to adopt. */
  offered: number;
  /** Its records the preview refuses. */
  refused: number;
}

/** The tools of `records`, as a dialog names and counts them. */
export const summariesOf = (records: readonly ToolRecords[]): ToolSummary[] =>
  records.map((t) => ({
    arg: t.arg,
    name: t.name,
    offered: t.candidates.length,
    refused: t.refused.length,
  }));

/** The run of the dialog: the rows it covers and how far it is. */
export interface AdoptRun {
  /** The tools it covers, as the dialog named them when Adopt was pressed. */
  tools: ToolSummary[];
  /** `running` until the last tool's run ends; `ended` with every result; `failed` when a run reported no skill. */
  phase: "running" | "ended" | "failed";
  rows: AdoptRow[];
  /** How many skills the dialog offered: its rows that were waiting when the run started. */
  total: number;
  /** How many skills the run has started to install: one `blobs` step each. */
  started: number;
  /** Why a run failed as a whole, before it reported any skill. */
  failure?: Problem;
  /** The warnings the runs logged that no row says. */
  warnings: string[];
}

/** The one adopt run, from Adopt until the dialog is closed after it: one at a time. */
export const useAdoptRun = create<{ run: AdoptRun | undefined }>()(() => ({
  run: undefined,
}));

const update = (change: (run: AdoptRun) => AdoptRun) => {
  useAdoptRun.setState(({ run }) => ({
    run: run === undefined ? run : change(run),
  }));
};

/** Forgets the run once its dialog closes. A run still going is never forgotten. */
export const dismissAdoptRun = () => {
  if (useAdoptRun.getState().run?.phase !== "running") {
    useAdoptRun.setState({ run: undefined });
  }
};

/** "Adopting 2 of 3" in skills, or "Adopting…" before the first skill, while sources are fetched. */
export const progressText = ({ started, total }: AdoptRun) =>
  started === 0
    ? "Adopting…"
    : `Adopting ${Math.min(started, total)} of ${total}`;

/** What a run adopted out of the skills it offered: "Adopted 2 of 3 skills". */
const adoptedText = (adopted: number, offered: number) => {
  if (adopted === 0) {
    return "No skills were adopted";
  }
  return adopted >= offered
    ? `Adopted ${plural(adopted, "skill")}`
    : `Adopted ${adopted} of ${plural(offered, "skill")}`;
};

/** The dialog's line once the run ended: how many of the skills it offered were adopted. */
export const summaryText = (run: AdoptRun) =>
  adoptedText(run.rows.filter((r) => r.status === "adopted").length, run.total);

/** The `adoption` events of a run. */
const adoptionsOf = (events: CliEvent[]) =>
  events.filter((e) => e.type === "adoption");

/**
 * The activity entry of one tool's run that reported its skills: how many of those it offered it
 * adopted, `success` when it adopted them all. A record the preview refused is not counted: the
 * run covers it too, and exits non-zero for it, whatever became of the others.
 */
const activityOf = (
  events: CliEvent[],
  offered: ReadonlySet<string>
): ActivityText | undefined => {
  const adoption = adoptionsOf(events);
  if (adoption.length === 0) {
    return undefined;
  }
  const adopted = adoption.filter(
    (e) => e.state === "adopted" && offered.has(e.name)
  ).length;
  return {
    message: adoptedText(adopted, offered.size),
    tone: adopted >= offered.size ? "success" : "attention",
  };
};

/** A step of the run that names a skill moves its row on. */
const stepped = (row: AdoptRow, phase: string): AdoptRow => {
  if (row.status !== "waiting" && row.status !== "running") {
    return row;
  }
  return { ...row, status: phase === "install" ? "installed" : "running" };
};

/** A row as the run's `adoption` event for it says; a row of another tool is left as it is. */
const settled = (
  row: AdoptRow,
  tool: ToolArg,
  adoption: AdoptionEvent[]
): AdoptRow => {
  if (row.tool !== tool) {
    return row;
  }
  const event = adoption.find((e) => e.name === row.name);
  if (event?.state === "adopted" || event?.state === "managed") {
    return { name: row.name, status: "adopted", tool };
  }
  const reason = event?.reason ?? row.reason;
  return {
    name: row.name,
    status: row.status === "left-out" ? "left-out" : "failed",
    tool,
    ...(reason === undefined ? {} : { reason }),
  };
};

/** The warnings a run logged, but those that only repeat a row's reason ("<name>: <reason>"). */
const otherWarnings = (outcome: Outcome) => {
  const said = new Set(
    adoptionsOf(outcome.events).flatMap((e) =>
      e.reason === undefined
        ? []
        : [e.reason, `${e.name}: ${e.reason}`, `${e.name} ${e.reason}`]
    )
  );
  return outcome.logs
    .filter((l) => l.level === "warn")
    .map((l) => l.message)
    .filter((m) => !said.has(m));
};

/** A run that failed as a whole: the CLI's problem, its hint in the app's words. */
const failureOf = (outcome: Outcome): Problem => {
  const error = outcome.events.find((e) => e.type === "error");
  if (error === undefined) {
    return {
      message:
        outcome.startError === undefined
          ? "agentx adopt did not finish"
          : `agentx could not start: ${outcome.startError}`,
    };
  }
  const hint = rewriteHint(error.hint, [doctorRule, againRule("try again")]);
  return hint === undefined
    ? { message: error.message }
    : { hint, message: error.message };
};

/** Whether a run ended in a way the runner answers itself: the lock, the account repo, a cancel. */
const answeredByRunner = (outcome: Outcome) =>
  outcome.exitCode === EXIT_CODES.locked ||
  outcome.exitCode === EXIT_CODES.account_repo ||
  outcome.cancelled;

/**
 * Runs one tool's `adopt <tool> --all` and settles its rows. Returns false when the run did not
 * report its skills, and the runs after it should not start.
 */
const adoptTool = async (tool: ToolArg, rows: AdoptRow[]) => {
  const offered = new Set(
    rows
      .filter((r) => r.tool === tool && r.status === "waiting")
      .map((r) => r.name)
  );
  const outcome = await runUntilSettled(["adopt", tool, "--all"], {
    label: `adopt all ${tool}`,
    onProgress: (progress) => {
      const { phase, subject } = progress;
      update((run) => ({
        ...run,
        rows: run.rows.map((row) =>
          row.tool === tool && row.name === subject ? stepped(row, phase) : row
        ),
        started: run.started + (phase === "blobs" ? 1 : 0),
      }));
    },
    ownActivity: (failed) => activityOf(failed.events, offered),
    ownFailure: true,
  });
  const adoption = adoptionsOf(outcome.events);
  if (outcome.exitCode === EXIT_CODES.ok) {
    const text = activityOf(outcome.events, offered);
    const warnings = otherWarnings(outcome);
    if (text !== undefined) {
      addActivity(warnings.length > 0 ? "attention" : text.tone, text.message);
    }
  }
  if (adoption.length > 0) {
    update((run) => ({
      ...run,
      rows: run.rows.map((row) => settled(row, tool, adoption)),
      warnings: [...run.warnings, ...otherWarnings(outcome)],
    }));
    return true;
  }
  if (outcome.exitCode === EXIT_CODES.ok) {
    return true;
  }
  // Nothing was reported: the rows stay as they were before the run.
  const before = (row: AdoptRow): AdoptRow =>
    row.tool === tool && offered.has(row.name)
      ? { name: row.name, status: "waiting", tool }
      : row;
  if (answeredByRunner(outcome)) {
    // The runner said why; the dialog goes back to before the run, so Adopt can run again.
    useAdoptRun.setState({ run: undefined });
    return false;
  }
  update((run) => ({
    ...run,
    failure: failureOf(outcome),
    phase: "failed",
    rows: run.rows.map(before),
  }));
  return false;
};

/**
 * Adopts every skill of `rows` that is waiting, one tool after another, in the dialog's order.
 * The preview is read again afterwards; the inventory follows each run.
 */
export const adoptAll = async (tools: ToolSummary[], rows: AdoptRow[]) => {
  if (useAdoptRun.getState().run !== undefined) {
    return;
  }
  useAdoptRun.setState({
    run: {
      phase: "running",
      rows,
      started: 0,
      tools,
      total: rows.filter((r) => r.status === "waiting").length,
      warnings: [],
    },
  });
  let going = true;
  for (const { arg } of tools) {
    if (going) {
      // One tool after another: the CLI's lock admits one run at a time.
      // oxlint-disable-next-line no-await-in-loop
      going = await adoptTool(arg, rows);
    }
  }
  update((run) => (run.phase === "running" ? { ...run, phase: "ended" } : run));
  await loadAdoption();
};
