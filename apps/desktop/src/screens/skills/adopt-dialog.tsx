// The adopt dialog: what adopting the skills of one tool, or of both, would do, the skills it
// covers by name (the ones the preview refuses marked from the start, with the CLI's reason), then
// Cancel · Adopt. Adopt runs in the dialog (`adopt.ts`): each row shows its state, then its result,
// with the reason of a skill the CLI did not adopt; one line counts the skills started, then says
// how many were adopted. The dialog cannot be dismissed while the run goes, and Done closes it.
import { CheckIcon } from "#/components/icons/check.tsx";
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
  adopted: "Adopted",
  failed: "Not adopted",
  installed: "Installed",
  "left-out": "Can't be adopted",
  running: "Adopting",
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
            {problemSentence({ message: row.reason }, { keep: [row.name] })}
          </p>
        )}
    </div>
  </li>
);

/** The skills of the dialog, by tool when it covers two, in a list that scrolls past its height. */
const Rows = ({
  rows,
  tools,
}: {
  rows: AdoptRow[];
  tools: readonly ToolSummary[];
}) => (
  <section
    aria-label="Skills"
    className="max-h-adopt-list flex flex-col gap-4 overflow-auto"
  >
    {tools.map((tool) => (
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

/** "Skills CLI", "Skills CLI and Skillshare". */
const namesOf = (tools: readonly { name: string }[]) =>
  tools.map((t) => t.name).join(" and ");

/** What adopting does, and to the other tool's own files: what the CLI contract says it does. */
const Description = ({ tools }: { tools: readonly ToolSummary[] }) => {
  const offered = tools.reduce((n, t) => n + t.offered, 0);
  const refused = tools.reduce((n, t) => n + t.refused, 0);
  const both = tools.length > 1;
  return (
    <>
      <DialogDescription>
        {`agentx installs ${plural(offered, "skill")} again from their sources into your library and manages them from now on: updates, agents and history.`}
      </DialogDescription>
      <p className="type-secondary text-dialog-subtitle m-0">
        {`${namesOf(tools)} ${both ? "keep their" : "keeps its"} own records and files: agentx only reads them. What ${both ? "they" : "it"} left in your library under each name is replaced, so edits made there are not kept.`}
        {refused > 0 &&
          ` ${refused} ${refused === 1 ? "skill can't be adopted and is" : "skills can't be adopted and are"} left out.`}
      </p>
    </>
  );
};

/** The dialog's title: the tool it adopts the skills of, or both. */
const titleOf = (tools: readonly { name: string }[]) =>
  tools.length === 1
    ? `Adopt ${namesOf(tools)} skills`
    : `Adopt skills from ${namesOf(tools)}`;

/**
 * The adopt dialog for `tools`, the arguments of `adopt` it covers, in the app's order. Before the
 * run it reads the preview; from Adopt on, the run's own rows.
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
        <Rows rows={rows} tools={tools} />
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
            <output className="type-secondary text-text-secondary flex min-w-0 flex-1 items-center">
              {run.phase === "running" ? progressText(run) : summaryText(run)}
            </output>
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
                disabled={running || rows.length === 0}
                aria-busy={running ? true : undefined}
                onClick={() => {
                  void adoptAll(tools, rows);
                }}
              >
                {running && <Spinner />}
                Adopt
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
