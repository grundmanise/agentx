// Adopting the skills another tool installed, all at once: which tools have skills to adopt, as
// the inventory lists them (`adoptable`), and the one run that adopts them, which the Adopt skills
// section of Settings and the Unmanaged group's Adopt all both start through the adopt dialog
// (`adopt-dialog.tsx`). The run is one
// `skill import --all` with a `--from` for each tool the dialog covers. Each skill of the dialog
// is a row whose state follows the run's `progress` events, and whose result is the `adoption`
// event the run reports for it. The dialog shows the result, so no toast does; the Activity log
// has the run's command line and how it ended.
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  AdoptionEvent,
  AdoptionTool,
  CliEvent,
  InventoryEvent,
} from "#/cli/events.gen.ts";
import { againRule, doctorRule, rewriteHint } from "#/lib/app-hint.ts";
import { plural } from "#/lib/plural.ts";
import { refusalText } from "#/screens/skills/placements.ts";
import { refusalOf, sorted, toolName } from "#/screens/skills/projection.ts";
import type { Refusal } from "#/screens/skills/projection.ts";
import { cancelCommand } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import { runUntilSettled } from "#/store/settle.ts";
import type { Problem } from "#/store/store.ts";

/** The value of `skill import --from` that names a tool. */
export type ToolArg = "skills-cli" | "skillshare";

/** A tool `skill import` takes the skills of: as the inventory names it, and its `--from` value. */
interface Tool {
  tool: AdoptionTool;
  arg: ToolArg;
}

/** The tools, in the order the app lists them. */
const TOOLS: Tool[] = [
  { arg: "skills-cli", tool: "vercel" },
  { arg: "skillshare", tool: "skillshare" },
];

/** One tool's skills in the inventory: the names to adopt and the skills `skill import` refuses. */
export interface ToolRecords {
  arg: ToolArg;
  /** The tool as the product calls it: "Skills CLI", "Skillshare". */
  name: string;
  candidates: string[];
  refused: Refusal[];
}

/** In the order of the CLI's lists: by code unit. */
const byName = (a: string, b: string) => (a < b ? -1 : Number(a > b));

/**
 * The tools with a skill to adopt, from the inventory's `adoptable`, in the app's order, each
 * with its skills of every source in name order.
 */
export const toolsToAdopt = (inventory: InventoryEvent | undefined) =>
  TOOLS.flatMap((tool): ToolRecords[] => {
    const entries = (inventory?.adoptable ?? []).filter(
      (a) => a.tool === tool.tool
    );
    const candidates = sorted(
      entries.flatMap((a) => a.skills),
      byName
    );
    return inventory === undefined || candidates.length === 0
      ? []
      : [
          {
            arg: tool.arg,
            candidates,
            name: toolName(tool.tool),
            refused: sorted(
              entries.flatMap((a) => a.refused ?? []),
              (a, b) => byName(a.name, b.name)
            ).map((r) => refusalOf(r, inventory.machine.library)),
          },
        ];
  });

/**
 * Where a skill of the dialog is. Before the run every skill to adopt is `waiting`; a step of the
 * run that names it makes it `running`, and its `install` step `installed`. When the run ends its
 * `adoption` event makes it `adopted` or `failed`. A skill the inventory lists as refused is
 * `left-out` from the start, with why.
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
  /**
   * Why the CLI does not adopt it. Before the run, the app's sentence for the inventory's reason,
   * which does not name the skill; after it, the run's own words, a refusal's message, which may
   * start with the name.
   */
  reason?: string;
}

/** The rows of a dialog over `tools`: each tool's skills to adopt, then the ones it refuses. */
export const rowsOf = (tools: readonly ToolRecords[]): AdoptRow[] =>
  tools.flatMap((t) => [
    ...t.candidates.map((name): AdoptRow => ({
      name,
      status: "waiting",
      tool: t.arg,
    })),
    ...t.refused.map((r): AdoptRow => ({
      name: r.name,
      reason: refusalText(r),
      status: "left-out",
      tool: t.arg,
    })),
  ]);

/** A tool a dialog covers, as it was when the dialog opened: its name and its counts. */
export interface ToolSummary {
  arg: ToolArg;
  name: string;
  /** Its skills to adopt. */
  offered: number;
  /** Its skills `skill import` refuses. */
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
  /** `running` until the run ends; `ended` with every result; `failed` when the run reported no skill. */
  phase: "running" | "ended" | "failed";
  rows: AdoptRow[];
  /** How many skills the dialog offered: its rows that were waiting when the run started. */
  total: number;
  /** How many skills the run has started to install: one `blobs` step each. */
  started: number;
  /** Why a run failed as a whole, before it reported any skill. */
  failure?: Problem;
  /** The warnings the run logged that no row says. */
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

/** The label of the run of the dialog. */
const ADOPT_ALL = "adopt all";

/** Stops the run of the dialog, if one runs. */
export const stopAdoptRun = () => {
  if (useAdoptRun.getState().run?.phase === "running") {
    void cancelCommand(ADOPT_ALL);
  }
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
    ? "Importing…"
    : `Importing ${Math.min(started, total)} of ${total}`;

/** What a run adopted out of the skills it offered: "Adopted 2 of 3 skills". */
const adoptedText = (adopted: number, offered: number) => {
  if (adopted === 0) {
    return "No skills were imported";
  }
  return adopted >= offered
    ? `Imported ${plural(adopted, "skill")}`
    : `Imported ${adopted} of ${plural(offered, "skill")}`;
};

/** The dialog's line once the run ended: how many of the skills it offered were adopted. */
export const summaryText = (run: AdoptRun) =>
  adoptedText(run.rows.filter((r) => r.status === "adopted").length, run.total);

/** The `adoption` events of a run. */
const adoptionsOf = (events: CliEvent[]) =>
  events.filter((e) => e.type === "adoption");

/** A step of the run that names a skill moves its row on. */
const stepped = (row: AdoptRow, phase: string): AdoptRow => {
  if (row.status !== "waiting" && row.status !== "running") {
    return row;
  }
  return { ...row, status: phase === "install" ? "installed" : "running" };
};

/** A row as the run's `adoption` event for it says. */
const settled = (row: AdoptRow, adoption: AdoptionEvent[]): AdoptRow => {
  const event = adoption.find((e) => e.name === row.name);
  if (event?.state === "adopted" || event?.state === "managed") {
    return { name: row.name, status: "adopted", tool: row.tool };
  }
  const reason = event?.reason ?? row.reason;
  return {
    name: row.name,
    status: row.status === "left-out" ? "left-out" : "failed",
    tool: row.tool,
    ...(reason === undefined ? {} : { reason }),
  };
};

/** How the CLI starts the warning it logs for a skill a run of every skill cannot adopt. */
const cannotAdopt = (name: string) => `${name} could not be imported: `;

/**
 * The warnings a run logged, but those about a skill it did not adopt, whose row says why: the
 * warning that the skill could not be imported ("<name> could not be imported: …"), and one that only
 * repeats a row's reason ("<name>: <reason>").
 */
const otherWarnings = (outcome: Outcome) => {
  const adoption = adoptionsOf(outcome.events);
  const said = new Set(
    adoption.flatMap((e) =>
      e.reason === undefined
        ? []
        : [e.reason, `${e.name}: ${e.reason}`, `${e.name} ${e.reason}`]
    )
  );
  const refused = adoption.filter((e) => e.state === "refused");
  return outcome.logs
    .filter((l) => l.level === "warn")
    .map((l) => l.message)
    .filter(
      (m) =>
        !said.has(m) && !refused.some((e) => m.startsWith(cannotAdopt(e.name)))
    );
};

/** A run that failed as a whole: the CLI's problem, its hint in the app's words. */
const failureOf = (outcome: Outcome): Problem => {
  const error = outcome.events.find((e) => e.type === "error");
  if (error === undefined) {
    return {
      message:
        outcome.startError === undefined
          ? "The import did not finish"
          : `agentx could not start: ${outcome.startError}`,
    };
  }
  const hint = rewriteHint(error.hint, [doctorRule, againRule("try again")]);
  return hint === undefined
    ? { message: error.message }
    : { hint, message: error.message };
};

/** Whether a run ended in a way the runner answers itself: the lock, the store, a cancel. */
const answeredByRunner = (outcome: Outcome) =>
  outcome.exitCode === EXIT_CODES.locked ||
  outcome.exitCode === EXIT_CODES.store ||
  outcome.cancelled;

/**
 * Adopts every skill of `rows` that is waiting, in one run of `skill import --all` over `tools`,
 * and settles the rows. The inventory follows it.
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
  const outcome = await runUntilSettled(
    ["skill", "import", ...tools.flatMap((t) => ["--from", t.arg]), "--all"],
    {
      label: ADOPT_ALL,
      onProgress: (progress) => {
        const { phase, subject } = progress;
        update((run) => ({
          ...run,
          rows: run.rows.map((row) =>
            row.name === subject ? stepped(row, phase) : row
          ),
          started: run.started + (phase === "blobs" ? 1 : 0),
        }));
      },
      ownFailure: true,
    }
  );
  const adoption = adoptionsOf(outcome.events);
  if (adoption.length > 0) {
    update((run) => ({
      ...run,
      phase: "ended",
      rows: run.rows.map((row) => settled(row, adoption)),
      warnings: otherWarnings(outcome),
    }));
    return;
  }
  if (outcome.exitCode === EXIT_CODES.ok) {
    update((run) => ({ ...run, phase: "ended" }));
    return;
  }
  if (answeredByRunner(outcome)) {
    // The runner said why; the dialog goes back to before the run, so Adopt can run again.
    useAdoptRun.setState({ run: undefined });
    return;
  }
  // Nothing was reported: the rows stay as they were before the run.
  update((run) => ({
    ...run,
    failure: failureOf(outcome),
    phase: "failed",
    rows,
  }));
};
