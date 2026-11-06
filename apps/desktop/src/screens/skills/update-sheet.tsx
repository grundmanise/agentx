// The What's new sheet: what an update changes, file by file, before the user takes it in. It
// reads `skill diff <name> --update` when it opens and shows one collapsible block per file, its
// status, its path and the patch in the Code pane with the diff tints. An update another of your
// machines published has no base version here, so the CLI cannot diff it: the sheet lists its
// files from the update check and says when the changes can be read.
import { cva } from "class-variance-authority";
import { useEffect, useState } from "react";

import type { FileStatus, UpdateAvailableEvent } from "#/cli/events.gen.ts";
import { CodePane, parsePatch } from "#/components/code-pane.tsx";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "#/components/ui/accordion.tsx";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "#/components/ui/sheet.tsx";
import { fromAnotherMachine } from "#/screens/skills/projection.ts";
import { loadUpdateDiff } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";

const TITLE = "What's new";

/** The note of an update from another machine, whose changes the CLI cannot diff. */
export const FILES_ONLY_NOTE = "agentx shows these changes once you update";

const statusVariants = cva("type-technical-sm flex-none", {
  variants: {
    status: {
      added: "text-text-caption",
      deleted: "text-accent-danger",
      modified: "text-text-caption",
    },
  },
});

const Status = ({ status }: { status: FileStatus }) => (
  <span className={statusVariants({ status })}>{status}</span>
);

const Path = ({ path }: { path: string }) => (
  <span className="type-technical text-file-row-path min-w-0 truncate">
    {path}
  </span>
);

const Hint = ({ children }: { children: string }) => (
  <p className="type-secondary text-text-hint m-0">{children}</p>
);

const BINARY = /^Binary files /mu;

/** One file's patch: its hunks in the Code pane, or what the patch says when it has none. */
const Patch = ({ path, patch }: { path: string; patch: string }) => {
  if (parsePatch(patch).length === 0) {
    return (
      <p className="bg-code-bg py-code px-file-row type-secondary text-text-hint m-0">
        {BINARY.test(patch) ? "Binary file" : "No line changes"}
      </p>
    );
  }
  return <CodePane path={path} label={`Changes to ${path}`} patch={patch} />;
};

/** The files of an update from another machine, as the update check lists them, with the note. */
const FilesOnly = ({ update }: { update: UpdateAvailableEvent }) => (
  <div className="flex flex-col gap-3">
    <Hint>{FILES_ONLY_NOTE}</Hint>
    <ul
      aria-label="Changed files"
      className="rounded-file-card border-card-border bg-card-bg m-0 flex list-none flex-col overflow-hidden border p-0"
    >
      {update.files.map((file) => (
        <li
          key={file.path}
          className="h-file-row px-file-row border-file-row-divider flex items-center gap-3 border-b last:border-b-0"
        >
          <Status status={file.status} />
          <Path path={file.path} />
        </li>
      ))}
    </ul>
  </div>
);

/** What the update changes, from `skill diff <name> --update`, one block per file, all open. */
const Changes = ({ update }: { update: UpdateAvailableEvent }) => {
  const read = useAppStore((s) => s.updateDiffs[update.name]);
  // Every block starts open; these are the ones the user closed.
  const [closed, setClosed] = useState<string[]>([]);

  useEffect(() => {
    void loadUpdateDiff(update.name, update.candidate);
  }, [update.name, update.candidate]);

  // A read of another candidate is not what this update changes.
  if (read === undefined || read.candidate !== update.candidate) {
    return <Hint>Reading the changes…</Hint>;
  }
  if (read.failed === true) {
    return (
      <div className="flex flex-col gap-1">
        <Hint>The changes could not be read</Hint>
        <p className="type-technical-sm text-text-caption m-0 break-all">
          {read.problem.message}
        </p>
      </div>
    );
  }
  const { diffs } = read;
  if (diffs.length === 0) {
    return <Hint>No changes to show</Hint>;
  }
  const paths = diffs.map((d) => d.path);
  const open = paths.filter((p) => !closed.includes(p));
  return (
    <Accordion
      label="Changes"
      value={open}
      onValueChange={(value) => {
        setClosed(paths.filter((p) => !value.includes(p)));
      }}
    >
      {diffs.map((diff) => (
        <AccordionItem key={diff.path} value={diff.path}>
          <AccordionTrigger label={diff.path} open={open.includes(diff.path)}>
            <Status status={diff.status} />
            <Path path={diff.path} />
          </AccordionTrigger>
          <AccordionContent>
            <Patch path={diff.path} patch={diff.patch} />
          </AccordionContent>
        </AccordionItem>
      ))}
    </Accordion>
  );
};

/**
 * The What's new sheet of `update`: its skill's name, `versions` (the base and the newer version,
 * as the update's banner and pill name them), then what it changes. Escape or Close closes it.
 */
export const UpdateSheet = ({
  update,
  versions,
  open,
  onOpenChange,
}: {
  update: UpdateAvailableEvent;
  versions: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => (
  <Sheet open={open} onOpenChange={onOpenChange}>
    <SheetContent size="wide">
      <SheetHeader>
        <SheetTitle>{TITLE}</SheetTitle>
        <SheetDescription>{update.name}</SheetDescription>
        <span className="type-technical-sm text-text-caption">{versions}</span>
      </SheetHeader>
      {/* The blocks scroll down only; the Code pane wraps a long line. */}
      <div className="min-h-0 flex-1 overflow-auto">
        {fromAnotherMachine(update) ? (
          <FilesOnly update={update} />
        ) : (
          <Changes update={update} />
        )}
      </div>
    </SheetContent>
  </Sheet>
);
