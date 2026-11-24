// The What's new sheet: what an update changes, file by file, before the user takes it in. It
// reads `skill diff <name> --update` when it opens and shows one collapsible block per file, its
// status, its path and the patch in the Code pane with the diff tints. An update another of your
// machines published has no base version here, so the CLI cannot diff it: the sheet lists its
// files from the update check and says when the changes can be read.
import { cva } from "class-variance-authority";
import { useEffect, useState } from "react";

import type {
  ChangedFile,
  FileStatus,
  UpdateAvailableEvent,
} from "#/cli/events.gen.ts";
import { CodePane, codeLines, parsePatch } from "#/components/code-pane.tsx";
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
import { cn } from "#/lib/utils.ts";
import { LoadingPane } from "#/screens/skills/files.tsx";
import { fromAnotherMachine } from "#/screens/skills/projection.ts";
import { loadUpdateDiff } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";

const TITLE = "What's new";

/** The note of an update from another machine, whose changes the CLI cannot diff. */
export const FILES_ONLY_NOTE =
  "Changes from another machine can't be shown before you update.";

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

const READING = "Reading the changes…";

const BINARY = /^Binary files /mu;

/**
 * The most lines a patch has that keeps its block as high as all of them in a short window: they
 * fit in `code.min-height`, so the block never gives up height for the others.
 */
const SHORT_PATCH_LINES = 7;

/** Whether the block of `patch` gives up height, down to its minimum, when the blocks overflow. */
const fillOf = (patch: string) =>
  codeLines({ patch }).length > SHORT_PATCH_LINES ? "scroll" : "content";

/** One file's patch: its hunks in the Code pane, or what the patch says when it has none. */
const Patch = ({ path, patch }: { path: string; patch: string }) => {
  if (parsePatch(patch).length === 0) {
    return (
      <p className="bg-code-bg py-code px-file-row type-secondary text-text-hint m-0">
        {BINARY.test(patch) ? "Binary file" : "No line changes"}
      </p>
    );
  }
  return (
    <CodePane
      path={path}
      label={`Changes to ${path}`}
      patch={patch}
      height="fill"
    />
  );
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

/**
 * The changes while they are read: the block of every file the update check lists, open unless
 * the user closed it, over the loading pane, so the blocks keep their place when the patches come
 * in. The first block's pane has the spinner.
 */
const Reading = ({
  files,
  closed,
  onClosedChange,
}: {
  files: ChangedFile[];
  closed: string[];
  onClosedChange: (closed: string[]) => void;
}) => {
  if (files.length === 0) {
    return <LoadingPane label={READING} />;
  }
  const paths = files.map((f) => f.path);
  const open = paths.filter((p) => !closed.includes(p));
  return (
    <Accordion
      label="Changes"
      fill
      value={open}
      onValueChange={(value) => {
        onClosedChange(paths.filter((p) => !value.includes(p)));
      }}
    >
      {files.map((file, i) => (
        <AccordionItem key={file.path} value={file.path} fill="scroll">
          <AccordionTrigger
            track="update-sheet.toggle-file"
            label={file.path}
            open={open.includes(file.path)}
          >
            <Status status={file.status} />
            <Path path={file.path} />
          </AccordionTrigger>
          <AccordionContent fill>
            <LoadingPane label={READING} height="fill" spinner={i === 0} />
          </AccordionContent>
        </AccordionItem>
      ))}
    </Accordion>
  );
};

/**
 * What the update changes, from `skill diff <name> --update`, one block per file, all open. A
 * block is as high as its lines. When the blocks overflow the sheet, a long patch's block gives up
 * height, down to `file.open-min-height`, and the sheet scrolls past that.
 */
const Changes = ({ update }: { update: UpdateAvailableEvent }) => {
  const stored = useAppStore((s) => s.updateDiffs[update.name]);
  // A read of another candidate is not what this update changes.
  const read = stored?.candidate === update.candidate ? stored : undefined;
  // Every block starts open; these are the ones the user closed.
  const [closed, setClosed] = useState<string[]>([]);
  // Patches that come in while the loading panes show cross-fade into them, as in the skill's
  // Files; one read before the sheet opened shows at once.
  const [shown, setShown] = useState(read);
  const [revealing, setRevealing] = useState(false);
  if (read !== shown) {
    setShown(read);
    if (shown === undefined && read?.failed !== true) {
      setRevealing(true);
    }
  }

  useEffect(() => {
    void loadUpdateDiff(update.name, update.candidate, update.base_hash);
  }, [update.name, update.candidate, update.base_hash]);

  if (read === undefined) {
    return (
      <Reading
        files={update.files}
        closed={closed}
        onClosedChange={setClosed}
      />
    );
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
      fill
      value={open}
      onValueChange={(value) => {
        setClosed(paths.filter((p) => !value.includes(p)));
      }}
    >
      {diffs.map((diff, i) => (
        <AccordionItem
          key={diff.path}
          value={diff.path}
          fill={fillOf(diff.patch)}
        >
          <AccordionTrigger
            track="update-sheet.toggle-diff"
            label={diff.path}
            open={open.includes(diff.path)}
          >
            <Status status={diff.status} />
            <Path path={diff.path} />
          </AccordionTrigger>
          <AccordionContent fill>
            {/* One tree, faded in or not, so that the pane is not remounted when the fade ends. */}
            <div className="relative flex min-h-0 flex-col">
              <div
                className={cn(
                  "flex min-h-0 flex-col",
                  revealing && "animate-code-reveal"
                )}
              >
                <Patch path={diff.path} patch={diff.patch} />
              </div>
              {revealing && (
                <LoadingPane
                  leaving
                  height="fill"
                  spinner={i === 0}
                  onLeft={() => {
                    setRevealing(false);
                  }}
                />
              )}
            </div>
          </AccordionContent>
        </AccordionItem>
      ))}
    </Accordion>
  );
};

/**
 * The What's new sheet of `update`: its skill's name, `versions` (the base and the newer version,
 * as the update's banner and pill name them), then what it changes. Escape or Close closes it.
 * `over` is `dialog` when it opens from the Update skills dialog, above it. It has no trigger, so
 * `onCloseAutoFocus` puts the focus back on what opened it.
 */
export const UpdateSheet = ({
  update,
  versions,
  open,
  onOpenChange,
  onCloseAutoFocus,
  over,
}: {
  update: UpdateAvailableEvent;
  versions: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCloseAutoFocus: (event: Event) => void;
  over?: "page" | "dialog";
}) => (
  <Sheet open={open} onOpenChange={onOpenChange}>
    <SheetContent
      name="whats-new"
      over={over}
      onCloseAutoFocus={onCloseAutoFocus}
    >
      <SheetHeader>
        <SheetTitle>{TITLE}</SheetTitle>
        <SheetDescription>{update.name}</SheetDescription>
        <span className="type-technical-sm text-text-caption">{versions}</span>
      </SheetHeader>
      {/* The blocks scroll down only; the Code pane wraps a long line. */}
      <div className="flex min-h-0 flex-1 flex-col overflow-auto">
        {fromAnotherMachine(update) ? (
          <FilesOnly update={update} />
        ) : (
          <Changes update={update} />
        )}
      </div>
    </SheetContent>
  </Sheet>
);
