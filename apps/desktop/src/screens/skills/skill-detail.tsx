import { useEffect, useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { InlineMarkdown } from "#/components/inline-markdown.tsx";
import { SelectableLink } from "#/components/selectable-link.tsx";
import { SkillGlyph } from "#/components/skill-glyph.tsx";
import { BadgeButton } from "#/components/ui/badge.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { openInEditor } from "#/lib/editor.ts";
import { openInBrowser } from "#/lib/open-url.ts";
import { revealInFinder, showInLabel } from "#/lib/reveal.ts";
import { AgentsSection } from "#/screens/skills/agents-section.tsx";
import { BannerView } from "#/screens/skills/banner-view.tsx";
import { bannerFor } from "#/screens/skills/banners.ts";
import { detailsOf, originPage } from "#/screens/skills/details.ts";
import type { Detail } from "#/screens/skills/details.ts";
import { canEdit, skillFile } from "#/screens/skills/edit.ts";
import { FilesSection } from "#/screens/skills/files.tsx";
import { ForkButton } from "#/screens/skills/fork-dialog.tsx";
import { canFork } from "#/screens/skills/fork.ts";
import { HistorySection } from "#/screens/skills/history.tsx";
import { openSkill } from "#/screens/skills/open.ts";
import {
  filesVersionOf,
  glyphOf,
  glyphTitle,
  originOf,
  updateKey,
} from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { RemoveDialog, removeLabel } from "#/screens/skills/remove-dialog.tsx";
import { RenameButton } from "#/screens/skills/rename-dialog.tsx";
import { canRename } from "#/screens/skills/rename.ts";
import { SightsNotice } from "#/screens/skills/sights-view.tsx";
import { sightsOf } from "#/screens/skills/sights.ts";
import { loadUpdateDiff } from "#/store/loaders.ts";
import { useSettling } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";

const Caption = ({ children }: { children: string }) => (
  <h2 className="type-caption text-text-caption m-0">{children}</h2>
);

/**
 * A value of DETAILS: a link that opens its URL in the browser, or shows its directory in Finder,
 * when it has one. Like every value, it is selectable.
 */
const DetailValue = ({ detail }: { detail: Detail }) => {
  const { link, value } = detail;
  if (link === undefined) {
    return value;
  }
  const page = link.kind === "page";
  return (
    <SelectableLink
      href={page ? link.url : `file://${link.path}`}
      title={page ? link.url : showInLabel()}
      tone="secondary"
      onOpen={() => {
        if (page) {
          void openInBrowser(link.url);
        } else {
          void revealInFinder(link.path);
        }
      }}
    >
      {value}
    </SelectableLink>
  );
};

/** Where the skill came from, beside its name: a link to its source's page when it has one. */
const Origin = ({ row }: { row: SkillRow }) => {
  const text = originOf(row);
  const page = originPage(row);
  if (page === undefined) {
    return text;
  }
  return (
    <SelectableLink
      href={page}
      title={page}
      tone="caption"
      onOpen={() => {
        void openInBrowser(page);
      }}
    >
      {text}
    </SelectableLink>
  );
};

/** Remove, a danger ghost for every library skill; pending until the skill has left the inventory. */
const RemoveButton = ({
  row,
  onClick,
}: {
  row: SkillRow;
  onClick: () => void;
}) => {
  const pending = useSettling(removeLabel(row));
  return (
    <Button
      variant="danger"
      size="sm"
      disabled={pending}
      aria-busy={pending ? true : undefined}
      onClick={onClick}
    >
      {pending && <Spinner />}
      Remove
    </Button>
  );
};

/**
 * Edit, a secondary small button for a skill the user may write to (`canEdit`): it opens the
 * skill's SKILL.md in the editor chosen in Settings and runs no command. Left out until `machine`
 * says where the library is.
 */
const EditButton = ({ row }: { row: SkillRow }) => {
  const library = useAppStore((s) => s.machine?.library);
  if (library === undefined) {
    return null;
  }
  return (
    <Button
      variant="secondary"
      size="sm"
      onClick={() => {
        void openInEditor(skillFile(library, row.name));
      }}
    >
      Edit
    </Button>
  );
};

/**
 * The right pane: what the CLI reports about one skill. Header: the title row with the name, origin
 * (unless the banner says where the skill came from), a pill per fork made from it under a new
 * name, which opens that fork, and at its right end the action row (Edit for a skill the user may
 * write to, Fork for a skill of a source or a plugin's, Rename for a hub skill, Remove for a
 * library skill; the other actions come with the changes that add them); then the description. No
 * status pill: the banner is the status. One banner, with its button; AGENTS, whose chips add and
 * remove placements, and, when some agent sees more than one skill of this name, a notice under the
 * chips whose Details shows what each agent sees; FILES; then DETAILS and HISTORY side by side. Every control runs one
 * command and redraws from the inventory that follows.
 */
export const SkillDetail = ({
  row,
  forks,
  sameName,
  configurations,
}: {
  row: SkillRow;
  /** The forks made from this skill under a new name. */
  forks: SkillRow[];
  /** The other skills on the machine with this skill's name. */
  sameName: SkillRow[];
  configurations: Configuration[];
}) => {
  // Which control opened the Remove dialog: the Universal chip says why the skill leaves every agent.
  // What opened the Remove dialog. It stays while the dialog closes, so its text does not change.
  const [removing, setRemoving] = useState<"button" | "universal">("button");
  const [removeOpen, setRemoveOpen] = useState(false);
  const sources = useAppStore((s) => s.sources);
  // How many files `skill diff` found changed, from the FILES read of this version of the skill.
  const changedFiles = useAppStore((s) => {
    const read = s.files[row.key];
    return read?.failed === undefined && read?.version === filesVersionOf(row)
      ? read.diffs?.length
      : undefined;
  });
  const conflicts = useAppStore((s) => s.conflicts);
  // The files the update changes, from `skill diff <name> --update` of this update.
  const update = row.update === undefined ? undefined : updateKey(row.update);
  const updateDiffs = useAppStore((s) => {
    const read = s.updateDiffs[row.name];
    return read?.candidate === update ? read?.diffs : undefined;
  });
  useEffect(() => {
    if (update !== undefined) {
      void loadUpdateDiff(row.name, update);
    }
  }, [row.name, update]);
  const banner = bannerFor(row, configurations, {
    changedFiles,
    conflicts,
    sources,
    updateFiles:
      updateDiffs === undefined
        ? undefined
        : [...new Set(updateDiffs.map((d) => d.path))],
  });
  const details = detailsOf(row, configurations);
  const sights = sightsOf(row, sameName, configurations);
  return (
    <article
      aria-label={row.name}
      className="pt-detail-top px-detail pb-detail-bottom max-w-detail gap-detail-section flex flex-col"
    >
      <header className="gap-detail-header flex flex-col">
        <div className="flex flex-wrap items-center gap-3">
          <SkillGlyph
            shape={glyphOf(row)}
            update={row.hasUpdate}
            title={glyphTitle(row)}
            size={18}
          />
          <h1 className="type-skill-name text-text-primary m-0 break-all select-text">
            {row.name}
          </h1>
          {/* Left out when the banner says where the skill came from. */}
          {banner?.coversOrigin !== true && (
            <span className="type-technical text-text-caption">
              <Origin row={row} />
            </span>
          )}
          {forks.map((fork) => (
            <BadgeButton
              key={fork.key}
              tone="neutral"
              size="detail"
              aria-label={`Forked as ${fork.name}`}
              title={`Forked as ${fork.name}`}
              onClick={() => {
                openSkill(fork.key);
              }}
            >
              <SkillGlyph
                shape="fork"
                size={12}
                tone="inherit"
                title={`Forked as ${fork.name}`}
                decorative
              />
              {fork.name}
            </BadgeButton>
          ))}
          {/* The action row, at the title row's right end: Publish and the rest come with the
              changes that add them. */}
          <div
            aria-label="Actions"
            className="ml-auto flex flex-wrap items-center gap-1.5 empty:hidden"
          >
            {canEdit(row) && <EditButton row={row} />}
            {canFork(row) && <ForkButton row={row} />}
            {canRename(row) && <RenameButton row={row} />}
            {row.entry !== undefined && (
              <RemoveButton
                row={row}
                onClick={() => {
                  setRemoving("button");
                  setRemoveOpen(true);
                }}
              />
            )}
          </div>
        </div>
        <SightsNotice sights={sights} />
        {row.description !== "" && (
          <p className="type-detail-desc text-text-muted m-0 text-pretty select-text">
            <InlineMarkdown text={row.description} />
          </p>
        )}
      </header>
      {banner !== undefined && <BannerView row={row} banner={banner} />}
      <section aria-label="Agents" className="flex flex-col gap-3">
        <Caption>Agents</Caption>
        <AgentsSection
          row={row}
          configurations={configurations}
          onRemove={() => {
            setRemoving("universal");
            setRemoveOpen(true);
          }}
        />
      </section>
      {row.entry !== undefined && (
        <RemoveDialog
          row={row}
          configurations={configurations}
          open={removeOpen}
          fromUniversal={removing === "universal"}
          onClose={() => {
            setRemoveOpen(false);
          }}
        />
      )}
      <FilesSection
        row={row}
        configurations={configurations}
        heading={<Caption>Files</Caption>}
      />
      <div className="gap-detail-columns grid grid-cols-2">
        <section aria-label="Details" className="flex flex-col gap-3">
          <Caption>Details</Caption>
          <dl className="gap-detail-kv m-0 flex flex-col">
            {details.map((d, i) => (
              <div key={`${d.key}-${i}`} className="gap-detail-kv flex">
                <dt className="type-secondary text-text-hint w-detail-key flex-none">
                  {d.key}
                </dt>
                <dd className="type-technical-sm text-text-secondary m-0 min-w-0 flex-1 break-all">
                  <DetailValue detail={d} />
                </dd>
              </div>
            ))}
          </dl>
        </section>
        {/* Only hub skills keep a history; other skills show no section. */}
        {row.kind === "hub" && (
          <section aria-label="History" className="flex flex-col gap-3">
            <Caption>History</Caption>
            <HistorySection row={row} />
          </section>
        )}
      </div>
    </article>
  );
};
