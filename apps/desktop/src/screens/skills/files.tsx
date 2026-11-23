import { useEffect, useState } from "react";
import type { ReactNode } from "react";

import type {
  Configuration,
  DiffEvent,
  SkillFileEvent,
} from "#/cli/events.gen.ts";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { CodePane, codeLines } from "#/components/code-pane.tsx";
import { FolderIcon } from "#/components/icons/folder.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { SplitButton } from "#/components/split-button.tsx";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "#/components/ui/accordion.tsx";
import { Badge } from "#/components/ui/badge.tsx";
import { Button } from "#/components/ui/button.tsx";
import { DropdownMenuItem } from "#/components/ui/dropdown-menu.tsx";
import { revealInFinder, showInLabel } from "#/lib/reveal.ts";
import { cn } from "#/lib/utils.ts";
import { useKeptRead } from "#/screens/skills/kept-read.ts";
import {
  filesRequestOf,
  filesVersionOf,
  locationsOf,
} from "#/screens/skills/projection.ts";
import type { SkillLocation, SkillRow } from "#/screens/skills/projection.ts";
import { loadFiles } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";
import type { FilesRead } from "#/store/store.ts";

const SKILL_MD = "SKILL.md";

/** A file's size in words: bytes under a KiB, then KiB, then MiB with one decimal. */
export const formatSize = (bytes: number) => {
  if (bytes < 1024) {
    return bytes === 1 ? "1 byte" : `${bytes} bytes`;
  }
  if (bytes < 1024 * 1024) {
    return `${Math.round(bytes / 1024)} KiB`;
  }
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
};

const lines = (n: number) => (n === 1 ? "1 line" : `${n} lines`);

/** One row of the list: a file the CLI read, or one the skill's edits deleted, known from its diff. */
interface FileRow {
  path: string;
  file?: SkillFileEvent;
  diff?: DiffEvent;
}

/** What a changed file's row says beside its path, by its status in `skill diff`. */
export const fileStatusLabels: Record<DiffEvent["status"], string> = {
  added: "added",
  deleted: "removed",
  modified: "edited",
};

/** What the row says beside the path: the line count, "binary", or that the file is too large. */
const metaOf = (row: FileRow) => {
  const { file } = row;
  if (file === undefined) {
    // A deleted file: the lines of its old content, the ones the pane shows from the patch.
    return lines(
      codeLines({ patch: row.diff?.patch ?? "", tint: "removed" }).length
    );
  }
  if (file.truncated === true) {
    return `Too large to show (${formatSize(file.size)})`;
  }
  const text = file.binary ? "binary" : lines(file.lines ?? 0);
  return file.ignored === true ? `${text} · ignored` : text;
};

/** SKILL.md first, then every other path in the CLI's order, the deleted files among them. */
const rowsOf = (read: FilesRead & { failed?: undefined }): FileRow[] => {
  const byPath = new Map<string, FileRow>();
  for (const file of read.files) {
    byPath.set(file.path, { file, path: file.path });
  }
  for (const diff of read.diffs ?? []) {
    const row = byPath.get(diff.path);
    if (row === undefined) {
      if (diff.status === "deleted") {
        byPath.set(diff.path, { diff, path: diff.path });
      }
    } else {
      row.diff = diff;
    }
  }
  const rows = [...byPath.values()];
  rows.sort((a, b) => {
    if (a.path === SKILL_MD || b.path === SKILL_MD) {
      return a.path === SKILL_MD ? -1 : 1;
    }
    if (a.path === b.path) {
      return 0;
    }
    return a.path < b.path ? -1 : 1;
  });
  return rows;
};

const pathClass = (open: boolean) =>
  cn(
    "type-technical min-w-0 truncate",
    open ? "text-file-row-path-open" : "text-file-row-path"
  );

/** The pane of one row: the file, with its edits tinted, or the placeholder for what is not shown. */
const Pane = ({ row }: { row: FileRow }) => {
  const { file, diff } = row;
  if (file === undefined) {
    return (
      <CodePane
        path={row.path}
        label={`Content of ${row.path}`}
        patch={diff?.patch ?? ""}
        tint="removed"
      />
    );
  }
  if (file.truncated === true) {
    return (
      <p className="bg-code-bg py-code px-file-row type-secondary text-text-hint m-0">
        {`Too large to show (${formatSize(file.size)})`}
      </p>
    );
  }
  if (file.binary) {
    return (
      <p className="bg-code-bg py-code px-file-row type-secondary text-text-hint m-0">
        {`Binary file (${formatSize(file.size)})`}
      </p>
    );
  }
  const content = file.content ?? "";
  if (diff?.status === "added") {
    return (
      <CodePane
        path={row.path}
        label={`Content of ${row.path}`}
        content={content}
        tint="added"
      />
    );
  }
  return diff?.status === "modified" ? (
    <CodePane
      path={row.path}
      label={`Content of ${row.path}`}
      content={content}
      patch={diff.patch}
    />
  ) : (
    <CodePane
      path={row.path}
      label={`Content of ${row.path}`}
      content={content}
    />
  );
};

/** How many agents' logos a place of the menu stacks; "+n" stands for the rest. */
const MAX_PLACE_LOGOS = 4;

/**
 * What a place's first line says after it: Library for a symlink into the library, Symlink for
 * another, nothing for a copy.
 */
const SYMLINK_CAPTIONS = { library: "Library", other: "Symlink" } as const;
const captionOf = (location: SkillLocation) =>
  location.symlink
    ? SYMLINK_CAPTIONS[location.intoLibrary ? "library" : "other"]
    : undefined;

/** A place's caption: a neutral pill (§6 "Pills (status)") at the right of its first line. */
const Caption = ({ children }: { children: string }) => (
  <span className="ml-auto flex">
    <Badge tone="neutral">{children}</Badge>
  </span>
);

/**
 * The first line of a place in Show in Finder's menu, then its caption, if any: the library by
 * name; one agent by its logo and name; several, whose symlinks lead there, by their logos
 * stacked, whose "+n" lists every name beside the menu.
 */
const PlaceLine = ({
  clients,
  label,
  caption,
}: {
  clients: SkillLocation["clients"];
  label: string;
  caption: string | undefined;
}) => {
  const [only] = clients;
  if (clients.length > 1) {
    return (
      <span className="flex items-center gap-2 self-stretch">
        <LogoStack
          clients={clients.slice(0, MAX_PLACE_LOGOS)}
          hidden={clients.slice(MAX_PLACE_LOGOS)}
          size={20}
          surface="overlay"
          listSide="left"
        />
        {caption === undefined ? undefined : <Caption>{caption}</Caption>}
      </span>
    );
  }
  return (
    <span className="flex items-center gap-2 self-stretch">
      {only === undefined ? undefined : (
        <AgentLogo client={only} size={20} decorative tooltip={false} />
      )}
      <span className="type-body text-text-primary">{label}</span>
      {caption === undefined ? undefined : <Caption>{caption}</Caption>}
    </span>
  );
};

/**
 * Show in Finder for one file: the button opens the file at the skill's first place, the library
 * for a skill the library holds; when the skill is elsewhere too, a three-dots button beside it opens a
 * menu of those places, each named by its agent client with its path, which opens the file there. A
 * symlink is marked as one, or as Library when it leads into the library, and shows the path it
 * leads to, the one it opens; symlinks of several
 * agent clients that lead to one place are one row.
 */
const Reveal = ({
  path,
  locations,
  onOpenChange,
}: {
  path: string;
  /** The first is where the button opens the file. */
  locations: SkillLocation[];
  /** Whether the menu is open, so that the row keeps the control shown. */
  onOpenChange: (open: boolean) => void;
}) => {
  const [first, ...others] = locations;
  if (first === undefined) {
    return null;
  }
  const show = () => {
    void revealInFinder(`${first.directory}/${path}`);
  };
  if (others.length === 0) {
    return (
      <Button
        variant="ghost"
        size="icon"
        aria-label={showInLabel(path)}
        title={showInLabel()}
        onClick={show}
      >
        <FolderIcon />
      </Button>
    );
  }
  return (
    <SplitButton
      size="icon"
      label={showInLabel(path)}
      title={showInLabel()}
      onClick={show}
      menuLabel={`${showInLabel(path)} elsewhere`}
      menuTitle={`${showInLabel()} elsewhere`}
      onOpenChange={onOpenChange}
      menu={others.map((location) => (
        <DropdownMenuItem
          key={location.key}
          aria-label={
            location.clients.length > 1
              ? `${location.label}, ${location.intoLibrary ? "library" : "symlink"}, ${location.directory}`
              : undefined
          }
          onSelect={() => {
            void revealInFinder(`${location.directory}/${path}`);
          }}
        >
          <PlaceLine
            clients={location.clients}
            label={location.label}
            caption={captionOf(location)}
          />
          <span className="type-technical-sm text-text-caption break-all">
            {location.symlink ? location.directory : location.path}
          </span>
        </DropdownMenuItem>
      ))}
    >
      <FolderIcon />
    </SplitButton>
  );
};

const Hint = ({ children }: { children: string }) => (
  <p className="type-secondary text-text-hint m-0">{children}</p>
);

/**
 * A stand-in for a SKILL.md, long enough to fill the Code pane, shown blurred while the files are
 * read. Its words are never read: it gives the blur the shape of a real file.
 */
const STAND_IN = `---
name: skill-name
description: What the skill does, and when an agent should use it.
---

# Skill name

A short paragraph that says what this skill is for and when to reach for it.

## When to use

- The first situation where the skill helps
- A second situation, described in a few words
- A third one, with a little more detail than the others

## How it works

1. Read the input and decide what to do with it
2. Do the work, one step at a time
3. Check the result before handing it back

## Notes

Anything else the agent should keep in mind while it works.
`;

/**
 * The SKILL.md pane while the files are read: a stand-in file blurred like frosted glass with a
 * spinner on top, at the Code pane's full height. `leaving`, it lies over the real SKILL.md as
 * that comes in and fades out, hidden from assistive technology, then calls `onLeft`. `label` is
 * what it says to assistive technology. `height` is the Code pane's: `capped` at `code.height`, or
 * `fill`, as high as the stand-in while its flex column has the room. `spinner` false leaves the spinner and its
 * status to another loading pane beside this one.
 */
export const LoadingPane = ({
  leaving = false,
  onLeft,
  label = "Reading the files…",
  height = "capped",
  spinner = true,
}: {
  leaving?: boolean;
  onLeft?: () => void;
  label?: string;
  height?: "capped" | "fill";
  spinner?: boolean;
}) => (
  <div
    aria-busy={leaving ? undefined : true}
    aria-hidden={leaving ? true : undefined}
    inert={leaving}
    className={cn(
      "bg-code-bg overflow-hidden",
      leaving &&
        "animate-code-loading-out pointer-events-none absolute inset-0 flex flex-col",
      !leaving && height === "capped" && "h-code relative",
      !leaving && height === "fill" && "relative flex min-h-0 flex-col"
    )}
    onAnimationEnd={(event) => {
      // The spinner turns inside; only the pane's own fade ends it.
      if (event.target === event.currentTarget) {
        onLeft?.();
      }
    }}
  >
    <div
      aria-hidden
      inert
      className="blur-code-loading pointer-events-none flex min-h-0 flex-col select-none"
    >
      <CodePane
        path={SKILL_MD}
        label={SKILL_MD}
        content={STAND_IN}
        height={height}
      />
    </div>
    {spinner && (
      <output className="text-text-muted absolute inset-0 flex items-center justify-center">
        <Spinner size="md" />
        <span className="sr-only">{label}</span>
      </output>
    )}
  </div>
);

/**
 * The list while the files are read: the SKILL.md row every skill has, over the loading pane, so
 * the list keeps its place when the files come in. It shares the list's open rows, so a row the
 * user closes stays closed.
 */
const Reading = ({
  open,
  onOpenChange,
}: {
  open: string[];
  onOpenChange: (open: string[]) => void;
}) => (
  <Accordion label="Files" value={open} onValueChange={onOpenChange}>
    <AccordionItem value={SKILL_MD}>
      <AccordionTrigger label={SKILL_MD} open={open.includes(SKILL_MD)}>
        <span className={pathClass(open.includes(SKILL_MD))}>{SKILL_MD}</span>
      </AccordionTrigger>
      <AccordionContent>
        <LoadingPane />
      </AccordionContent>
    </AccordionItem>
  </Accordion>
);

/**
 * FILES in the detail pane: every file of the skill from `skill files`, read when the skill is
 * selected, as an accordion with SKILL.md first and open. For a modified skill, `skill diff
 * <name>` says which files were edited, added or removed since the version it is compared with,
 * and each pane shows the edits tinted. `heading` is the section's caption.
 */
export const FilesSection = ({
  row,
  configurations,
  heading,
}: {
  row: SkillRow;
  /** The configurations, which name the places a skill is in. */
  configurations: Configuration[];
  heading: ReactNode;
}) => {
  const request = filesRequestOf(row);
  const version = filesVersionOf(row);
  const library = useAppStore((s) => s.machine?.library);
  const locations = locationsOf(row, configurations, library);
  const read = useAppStore((s) => s.files[row.key]);
  const [open, setOpen] = useState<string[]>([SKILL_MD]);
  // The file whose places menu is open, whose row keeps its control shown.
  const [menuOf, setMenuOf] = useState<string | undefined>();

  useEffect(() => {
    const wanted = filesRequestOf(row);
    if (wanted !== undefined) {
      void loadFiles(row.key, wanted, version);
    }
  }, [row, version]);

  // Files that come in while the loading pane shows cross-fade into it; files read before stay
  // until the new ones replace them.
  const [revealing, setRevealing] = useState(false);
  const current = useKeptRead(
    read !== undefined && read.version === version ? read : undefined,
    (first) => {
      setRevealing(first.failed !== true);
    }
  );

  let body: ReactNode;
  if (request === undefined) {
    body = <Hint>No files to show</Hint>;
  } else if (current === undefined) {
    body = <Reading open={open} onOpenChange={setOpen} />;
  } else if (current.failed === true) {
    body = <Hint>The files could not be read</Hint>;
  } else {
    const rows = rowsOf(current);
    body =
      rows.length === 0 ? (
        <Hint>No files</Hint>
      ) : (
        <Accordion label="Files" value={open} onValueChange={setOpen}>
          {rows.map((file) => {
            const isOpen = open.includes(file.path);
            return (
              <AccordionItem key={file.path} value={file.path}>
                <AccordionTrigger
                  label={file.path}
                  open={isOpen}
                  meta={
                    <span className="type-technical-sm text-file-row-meta flex-none">
                      {metaOf(file)}
                    </span>
                  }
                  actionShown={menuOf === file.path}
                  action={
                    // A file on disk can be shown where it is; a deleted file has no place.
                    file.file !== undefined && locations.length > 0 ? (
                      <Reveal
                        path={file.path}
                        locations={locations}
                        onOpenChange={(isMenuOpen) => {
                          setMenuOf(isMenuOpen ? file.path : undefined);
                        }}
                      />
                    ) : undefined
                  }
                >
                  <span className={pathClass(isOpen)}>{file.path}</span>
                  {file.diff !== undefined && (
                    <span className="type-technical-sm text-file-row-edited-label flex-none">
                      {fileStatusLabels[file.diff.status]}
                    </span>
                  )}
                </AccordionTrigger>
                <AccordionContent>
                  {/* One tree, faded in or not, so that the pane is not remounted when the
                      fade ends and a selection made during it stays. */}
                  <div className="relative">
                    <div
                      className={cn(
                        revealing &&
                          file.path === SKILL_MD &&
                          "animate-code-reveal"
                      )}
                    >
                      <Pane row={file} />
                    </div>
                    {revealing && file.path === SKILL_MD && (
                      <LoadingPane
                        leaving
                        onLeft={() => {
                          setRevealing(false);
                        }}
                      />
                    )}
                  </div>
                </AccordionContent>
              </AccordionItem>
            );
          })}
        </Accordion>
      );
  }

  return (
    <section aria-label="Files" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline gap-3">
        {heading}
        {current?.failed === undefined && current?.diffFailed === true && (
          <span className="type-technical-sm text-text-caption">
            The edits could not be read
          </span>
        )}
      </div>
      {body}
    </section>
  );
};
