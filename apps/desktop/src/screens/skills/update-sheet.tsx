// The What's new sheet: what an update changes, file by file, before the user takes it in. It
// reads `skill diff <name> --update` when it opens and shows one collapsible block per file, its
// status, its path and the patch in the Code pane with the diff tints. A hub skill takes one update
// at a time: what another of your machines published, else a newer version from its upstream.
import { cva } from "class-variance-authority";
import { useEffect, useState } from "react";

import type { FileStatus } from "#/cli/events.gen.ts";
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
import { updateKey } from "#/screens/skills/projection.ts";
import type { UpdateOffer } from "#/screens/skills/projection.ts";
import { loadUpdateDiff } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";

const TITLE = "What's new";

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

/**
 * What the update changes, from `skill diff <name> --update`, one block per file, all open. A
 * block is as high as its lines. When the blocks overflow the sheet, a long patch's block gives up
 * height, down to `file.open-min-height`, and the sheet scrolls past that.
 */
const Changes = ({ update }: { update: UpdateOffer }) => {
  const key = updateKey(update);
  const stored = useAppStore((s) => s.updateDiffs[update.name]);
  // A read of other versions is not what this update changes.
  const read = stored?.candidate === key ? stored : undefined;
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
    void loadUpdateDiff(update.name, key);
  }, [update.name, key]);

  if (read === undefined) {
    return <LoadingPane label={READING} />;
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
  update: UpdateOffer;
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
        <Changes update={update} />
      </div>
    </SheetContent>
  </Sheet>
);
