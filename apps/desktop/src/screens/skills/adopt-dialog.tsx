// The adopt dialog: what adopting the skills of one tool, or of both, would do, the skills it
// covers by name, the ones the preview refuses folded under the list with the CLI's reason, then
// Cancel · Import. Import runs in the dialog (`adopt.ts`): each row shows its state, then its result,
// with the reason of a skill the CLI did not adopt; one line counts the skills started, then says
// how many were adopted. The dialog cannot be dismissed while the run goes, and Done closes it.
import { useState } from "react";

import { CheckIcon } from "#/components/icons/check.tsx";
import { ChevronIcon } from "#/components/icons/chevron.tsx";
import { CloseIcon } from "#/components/icons/close.tsx";
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
  DialogStatus,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import { plural } from "#/lib/plural.ts";
import { problemSentence } from "#/lib/problem-text.ts";
import {
  adoptAll,
  dismissAdoptRun,
  progressText,
  rowsOf,
  summariesOf,
  summaryText,
  toolsToAdopt,
  useAdoptRun,
} from "#/screens/skills/adopt.ts";
import type {
  AdoptRow,
  RowStatus,
  ToolArg,
  ToolSummary,
} from "#/screens/skills/adopt.ts";
import { useAppStore } from "#/store/store.ts";

/** What a screen reader hears for each state, before the name. */
const statusNames: Record<RowStatus, string> = {
  adopted: "Imported",
  failed: "Not imported",
  installed: "Installed",
  "left-out": "Can't be imported",
  running: "Importing",
  waiting: "Waiting",
};

const StatusMark = ({ status }: { status: RowStatus }) => {
  switch (status) {
    case "running": {
      return (
        <span className="text-text-muted flex">
          <Spinner />
        </span>
      );
    }
    case "installed": {
      return (
        <span className="text-text-hint flex">
          <CheckIcon />
        </span>
      );
    }
    case "adopted": {
      return (
        <span className="text-tone-primary-fg flex">
          <CheckIcon />
        </span>
      );
    }
    case "failed":
    case "left-out": {
      return (
        <span className="text-tone-danger-fg flex">
          <CloseIcon />
        </span>
      );
    }
    case "waiting": {
      return <span className="size-4 flex-none" />;
    }
    default: {
      return null;
    }
  }
};

/** One skill: its state, its name, and why the CLI did not adopt it. */
const Row = ({ row }: { row: AdoptRow }) => (
  <li className="flex items-start gap-2.5">
    <span className="flex h-5 items-center">
      <StatusMark status={row.status} />
    </span>
    <div className="flex min-w-0 flex-1 flex-col gap-0.5">
      <span
        className={
          row.status === "waiting" || row.status === "left-out"
            ? "type-body text-text-secondary truncate"
            : "type-body text-text-primary truncate"
        }
      >
        <span className="sr-only">{`${statusNames[row.status]}: `}</span>
        {row.name}
      </span>
      {row.reason !== undefined &&
        (row.status === "failed" || row.status === "left-out") && (
          <p className="type-notice text-tone-danger-fg m-0">
            {problemSentence(
              { message: row.reason },
              // Only a run's refusal may start with the skill's name, which keeps its case.
              { keep: row.status === "failed" ? [row.name] : [] }
            )}
          </p>
        )}
    </div>
  </li>
);

/**
 * The skills the dialog adopts, by tool when it covers two, in a list that scrolls past its height
 * and gives up room when the window is short.
 */
const Rows = ({
  rows,
  tools,
}: {
  rows: AdoptRow[];
  tools: readonly ToolSummary[];
}) => (
  <section
    aria-label="Skills"
    className="max-h-adopt-list flex min-h-0 flex-col gap-4 overflow-auto"
  >
    {tools
      .filter((tool) => rows.some((r) => r.tool === tool.arg))
      .map((tool) => (
        <section
          key={tool.arg}
          aria-label={tool.name}
          className="flex flex-col gap-2"
        >
          {tools.length > 1 && (
            <h3 className="type-caption text-text-caption m-0">{tool.name}</h3>
          )}
          <ul className="m-0 flex list-none flex-col gap-2 p-0">
            {rows
              .filter((r) => r.tool === tool.arg)
              .map((row) => (
                <Row key={row.name} row={row} />
              ))}
          </ul>
        </section>
      ))}
  </section>
);

/**
 * The skills the preview refused, folded under the list behind one line that counts them; opened,
 * each shows the CLI's reason, in a list that scrolls past its height.
 */
const Refused = ({ rows }: { rows: AdoptRow[] }) => {
  const [open, setOpen] = useState(false);
  if (rows.length === 0) {
    return null;
  }
  return (
    <section
      aria-label="Can't be imported"
      className="border-overlay-border flex min-h-0 flex-col gap-2 border-t pt-3"
    >
      <div>
        <Button
          variant="plain"
          size="inline"
          aria-expanded={open}
          onClick={() => {
            setOpen((o) => !o);
          }}
        >
          <span
            className={
              open
                ? "text-text-caption flex h-5 rotate-90 items-center"
                : "text-text-caption flex h-5 items-center"
            }
          >
            <ChevronIcon size="xs" />
          </span>
          {`${plural(rows.length, "skill")} can't be imported`}
        </Button>
      </div>
      {open && (
        <ul className="max-h-adopt-list m-0 flex min-h-0 list-none flex-col gap-2 overflow-auto p-0">
          {rows.map((row) => (
            <Row key={`${row.tool}:${row.name}`} row={row} />
          ))}
        </ul>
      )}
    </section>
  );
};

/** "Skills CLI", "Skills CLI and Skillshare". */
const namesOf = (tools: readonly { name: string }[]) =>
  tools.map((t) => t.name).join(" and ");

/** What adopting does, and to the other tool's own files: what the CLI contract says it does. */
const Description = ({ tools }: { tools: readonly ToolSummary[] }) => {
  const offered = tools.reduce((n, t) => n + t.offered, 0);
  const one = offered === 1;
  const what = one ? "the skill" : `the ${plural(offered, "skill")}`;
  return (
    <DialogDescription>
      {`Manage ${what} you added with ${namesOf(tools)} in agentx. ${namesOf(tools)} ${tools.length > 1 ? "aren't" : "isn't"} affected, but any custom edits to ${one ? "this skill" : "these skills"} in your library will be lost.`}
    </DialogDescription>
  );
};

/** The dialog's title: the tool it adopts the skills of, or both. */
const titleOf = (tools: readonly { name: string }[]) =>
  `Import skills from ${namesOf(tools)}`;

/**
 * The adopt dialog for `tools`, the `--from` values of `skill import` it covers, in the app's
 * order. Before the run it reads the preview; from Adopt on, the run's own rows.
 */
export const AdoptDialog = ({
  tools: wanted,
  open,
  onOpenChange,
}: {
  tools: readonly ToolArg[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const adoption = useAppStore((s) => s.adoption);
  const run = useAdoptRun((s) => s.run);
  const records = toolsToAdopt(adoption).filter((t) => wanted.includes(t.arg));
  // Once Adopt is pressed, the dialog shows the run: its tools and rows as they were then.
  const tools = run?.tools ?? summariesOf(records);
  const rows = run?.rows ?? rowsOf(records);
  const running = run?.phase === "running";
  const offered = rows.filter((r) => r.status !== "left-out");
  const refused = rows.filter((r) => r.status === "left-out");
  const close = () => {
    if (running) {
      return;
    }
    dismissAdoptRun();
    onOpenChange(false);
  };
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          close();
        }
      }}
    >
      <DialogContent
        onEscapeKeyDown={(e) => {
          if (running) {
            e.preventDefault();
          }
        }}
        onInteractOutside={(e) => {
          if (running) {
            e.preventDefault();
          }
        }}
      >
        <DialogHeader showCloseButton={false}>
          <DialogTitle>{titleOf(tools)}</DialogTitle>
          <Description tools={tools} />
        </DialogHeader>
        <Rows rows={offered} tools={tools} />
        <Refused rows={refused} />
        {run?.failure !== undefined && <ProblemText problem={run.failure} />}
        {run !== undefined &&
          run.phase !== "running" &&
          run.warnings.map((warning) => (
            <p key={warning} className="type-notice text-tone-attention-fg m-0">
              {warning}
            </p>
          ))}
        <DialogFooter>
          {run !== undefined && (
            <DialogStatus>
              {run.phase === "running" ? progressText(run) : summaryText(run)}
            </DialogStatus>
          )}
          {run === undefined || running ? (
            <>
              <DialogClose asChild>
                <Button variant="ghost" disabled={running}>
                  Cancel
                </Button>
              </DialogClose>
              <Button
                variant="primary"
                disabled={running || offered.length === 0}
                aria-busy={running ? true : undefined}
                onClick={() => {
                  void adoptAll(tools, rows);
                }}
              >
                {running && <Spinner />}
                Import
              </Button>
            </>
          ) : (
            <Button variant="primary" onClick={close}>
              Done
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
