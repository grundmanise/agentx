import { useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { PillButton } from "#/components/pill.tsx";
import { SkillGlyph } from "#/components/skill-glyph.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { openInEditor } from "#/lib/editor.ts";
import { AgentsSection } from "#/screens/skills/agents-section.tsx";
import { BannerView } from "#/screens/skills/banner-view.tsx";
import { bannerFor } from "#/screens/skills/banners.ts";
import { detailsOf } from "#/screens/skills/details.ts";
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
} from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { RemoveDialog, removeLabel } from "#/screens/skills/remove-dialog.tsx";
import { useSettling } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";

const Caption = ({ children }: { children: string }) => (
  <h2 className="type-caption text-text-caption m-0">{children}</h2>
);

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
 * The right pane: what the CLI reports about one skill. Header: the title row with the name,
 * origin (unless the banner says where the skill came from), a pill per fork made from it under a
 * new name, which opens that fork, and at its right end the action row (Edit for a skill the user
 * may write to, Fork for a skill that is not yet yours, Remove for a library skill; the other
 * actions come with the changes that add them); then the description. No status pill: the banner is the status. One banner, with its
 * button; AGENTS, whose chips add and remove placements; FILES; then DETAILS and HISTORY side by
 * side. Every control runs one command and redraws from the inventory that follows.
 */
export const SkillDetail = ({
  row,
  forks,
  configurations,
}: {
  row: SkillRow;
  /** The forks made from this skill under a new name. */
  forks: SkillRow[];
  configurations: Configuration[];
}) => {
  const [removing, setRemoving] = useState(false);
  const sources = useAppStore((s) => s.sources);
  // How many files `skill diff` found changed, from the FILES read of this version of the skill.
  const changedFiles = useAppStore((s) => {
    const read = s.files[row.key];
    return read?.failed === undefined && read?.version === filesVersionOf(row)
      ? read.diffs?.length
      : undefined;
  });
  const conflicts = useAppStore((s) => s.conflicts);
  const banner = bannerFor(row, configurations, {
    changedFiles,
    conflicts,
    sources,
  });
  const details = detailsOf(row, configurations);
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
          <h1 className="type-skill-name text-text-primary m-0 break-all">
            {row.name}
          </h1>
          {/* Left out when the banner says where the skill came from. */}
          {banner?.coversOrigin !== true && (
            <span className="type-technical text-text-caption">
              {originOf(row)}
            </span>
          )}
          {forks.map((fork) => (
            <PillButton
              key={fork.key}
              tone="neutral"
              size="detail"
              label={`Forked as ${fork.name}`}
              title={`Forked as ${fork.name}`}
              icon={
                <SkillGlyph
                  shape="fork"
                  size={12}
                  tone="inherit"
                  title={`Forked as ${fork.name}`}
                  decorative
                />
              }
              onClick={() => {
                openSkill(fork.key);
              }}
            >
              {fork.name}
            </PillButton>
          ))}
          {/* The action row, at the title row's right end: Publish and the rest come with the
              changes that add them. */}
          <div
            aria-label="Actions"
            className="ml-auto flex flex-wrap items-center gap-1.5 empty:hidden"
          >
            {canEdit(row) && <EditButton row={row} />}
            {canFork(row) && <ForkButton row={row} />}
            {row.entry !== undefined && (
              <RemoveButton
                row={row}
                onClick={() => {
                  setRemoving(true);
                }}
              />
            )}
          </div>
        </div>
        {row.description !== "" && (
          <p className="type-detail-desc text-text-muted m-0 text-pretty">
            {row.description}
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
            setRemoving(true);
          }}
        />
      </section>
      {row.entry !== undefined && (
        <RemoveDialog
          row={row}
          configurations={configurations}
          open={removing}
          onOpenChange={setRemoving}
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
                  {d.value}
                </dd>
              </div>
            ))}
          </dl>
        </section>
        <section aria-label="History" className="flex flex-col gap-3">
          <Caption>History</Caption>
          <HistorySection row={row} />
        </section>
      </div>
    </article>
  );
};
