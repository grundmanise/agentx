import { useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { Pill, PillButton } from "#/components/pill.tsx";
import { SkillGlyph } from "#/components/skill-glyph.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { AgentsSection } from "#/screens/skills/agents-section.tsx";
import { BannerView } from "#/screens/skills/banner-view.tsx";
import { bannerFor } from "#/screens/skills/banners.ts";
import { detailsOf } from "#/screens/skills/details.ts";
import { FilesSection } from "#/screens/skills/files.tsx";
import { HistorySection } from "#/screens/skills/history.tsx";
import { openSkill } from "#/screens/skills/open.ts";
import {
  glyphLit,
  glyphOf,
  glyphTitle,
  originOf,
} from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { RemoveDialog, removeLabel } from "#/screens/skills/remove-dialog.tsx";
import { pillTones } from "#/screens/skills/skill-row.tsx";
import { useSettling } from "#/store/settle.ts";

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
 * The right pane: what the CLI reports about one skill. Header, origin, pills and a pill per fork
 * made from it under a new name, which opens that fork; description; the action row (Remove for a
 * library skill; the other actions come with the changes that add them); one banner, with its
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
  const banner = bannerFor(row, configurations);
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
            lit={glyphLit(row)}
            title={glyphTitle(row)}
            size={18}
          />
          <h1 className="type-skill-name text-text-primary m-0 break-all">
            {row.name}
          </h1>
          <span className="type-technical text-text-caption">
            {originOf(row)}
          </span>
          {row.pills.map((pill) => (
            <Pill key={pill.kind} tone={pillTones[pill.kind]} size="detail">
              {pill.label}
            </Pill>
          ))}
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
                  lit={false}
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
        </div>
        {row.description !== "" && (
          <p className="type-detail-desc text-text-muted m-0 text-pretty">
            {row.description}
          </p>
        )}
        {/* The action row: Fork, Publish and the rest come with the changes that add them. */}
        <div
          aria-label="Actions"
          className="flex items-center gap-1.5 empty:hidden"
        >
          {row.entry !== undefined && (
            <RemoveButton
              row={row}
              onClick={() => {
                setRemoving(true);
              }}
            />
          )}
        </div>
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
