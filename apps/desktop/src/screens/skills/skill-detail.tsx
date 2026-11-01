import { cva } from "class-variance-authority";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentChip } from "#/components/agent-chip.tsx";
import type { AgentChipState } from "#/components/agent-chip.tsx";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { AlertIcon } from "#/components/icons/alert.tsx";
import { Pill, PillButton } from "#/components/pill.tsx";
import { SkillGlyph } from "#/components/skill-glyph.tsx";
import { bannerFor } from "#/screens/skills/banners.ts";
import type { Banner } from "#/screens/skills/banners.ts";
import { detailsOf } from "#/screens/skills/details.ts";
import { HistorySection } from "#/screens/skills/history.tsx";
import { openSkill } from "#/screens/skills/open.ts";
import {
  glyphLit,
  glyphOf,
  glyphTitle,
  originOf,
} from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { pillTones } from "#/screens/skills/skill-row.tsx";

const Caption = ({ children }: { children: string }) => (
  <h2 className="type-caption text-text-caption m-0">{children}</h2>
);

const bannerVariants = cva(
  "rounded-banner py-banner pr-banner-right pl-banner gap-banner flex items-center border",
  {
    variants: {
      tone: {
        attention: "border-tone-attention-border bg-tone-attention-bg",
        danger: "border-tone-danger-border bg-tone-danger-bg",
        primary: "border-tone-primary-border bg-tone-primary-bg",
        yours: "border-tone-yours-border bg-tone-yours-bg",
      },
    },
  }
);

const tileVariants = cva(
  "rounded-card-tile flex size-8 flex-none items-center justify-center",
  {
    variants: {
      tone: {
        attention: "text-tone-attention-fg",
        danger: "text-tone-danger-fg",
        primary: "text-tone-primary-fg",
        yours: "text-tone-yours-fg",
      },
    },
  }
);

/** The one banner of the detail pane (DESIGN.md §6 "Banner"); its actions come in later changes. */
const BannerView = ({ banner }: { banner: Banner }) => (
  <div
    role="note"
    aria-label={banner.title}
    className={bannerVariants({ tone: banner.tone })}
  >
    <span className={tileVariants({ tone: banner.tone })}>
      <AlertIcon size="lg" />
    </span>
    <div className="gap-banner-text flex min-w-0 flex-1 flex-col">
      <span className="type-banner-title text-banner-title">
        {banner.title}
      </span>
      <span className="type-secondary text-banner-text">{banner.text}</span>
    </div>
    {/* The banner's actions go here. */}
    <div className="flex items-center gap-1.5" />
  </div>
);

/** Whether a configuration sees the skill: through a placement, displaced or not, or an occurrence. */
const chipState = (row: SkillRow, c: Configuration): AgentChipState => {
  const placement = row.entry?.placements.find((p) => p.configuration === c.id);
  if (placement !== undefined) {
    return placement.kind === placement.mode ? "on" : "displaced";
  }
  return row.occurrences.some((o) => o.configuration === c.id) ? "on" : "off";
};

/** AGENTS: one chip per configuration that does not read the library, then Universal. */
const Agents = ({
  row,
  configurations,
}: {
  row: SkillRow;
  configurations: Configuration[];
}) => {
  const direct = configurations.filter((c) => !c.reads_library);
  const universal = configurations.filter((c) => c.reads_library);
  return (
    <div className="gap-agent-chip-wrap flex flex-wrap">
      {direct.map((c) => (
        <AgentChip
          key={c.id}
          logo={<AgentLogo client={c} size={28} />}
          name={c.name}
          state={chipState(row, c)}
        />
      ))}
      {universal.length > 0 && (
        <AgentChip
          logo={<LogoStack clients={universal} size={28} />}
          name="Universal"
          state={row.universal ? "on" : "off"}
        />
      )}
    </div>
  );
};

/**
 * The right pane: what the CLI reports about one skill. Header, origin, pills and a pill per fork
 * made from it under a new name, which opens that fork; description; the action row (empty until
 * the changes that add actions); one banner; AGENTS; FILES; then DETAILS and HISTORY side by side.
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
  const banner = bannerFor(row);
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
        />
      </header>
      {banner !== undefined && <BannerView banner={banner} />}
      <section aria-label="Agents" className="flex flex-col gap-3">
        <Caption>Agents</Caption>
        <Agents row={row} configurations={configurations} />
      </section>
      <section aria-label="Files" className="flex flex-col gap-3">
        <Caption>Files</Caption>
        {/* The file tree and the code pane come in a later change. */}
      </section>
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
