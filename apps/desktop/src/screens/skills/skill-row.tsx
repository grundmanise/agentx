import type { Configuration } from "#/cli/events.gen.ts";
import { LogoStack } from "#/components/agent-logo.tsx";
import { ListRow } from "#/components/list-row.tsx";
import { Pill } from "#/components/pill.tsx";
import type { PillTone } from "#/components/pill.tsx";
import { SkillGlyph } from "#/components/skill-glyph.tsx";
import { glyphLit, glyphOf, glyphTitle } from "#/screens/skills/projection.ts";
import type { PillKind, SkillRow } from "#/screens/skills/projection.ts";

/** How many agent client logos a row shows before "+n". */
const MAX_LOGOS = 4;

export const pillTones: Record<PillKind, PillTone> = {
  "not-published": "neutral",
  "removed-from-source": "removed",
  "source-removed": "removed",
  "update-waiting": "update",
};

/**
 * One skill in the list: glyph, name, pills, a small fork marker for each fork made from it under
 * a new name, and the logos of the agent clients that see it, then its description. Selecting it
 * shows the skill in the detail pane.
 */
export const SkillRowItem = ({
  row,
  forks,
  configurations,
  selected,
  onSelect,
}: {
  row: SkillRow;
  /** The forks made from this skill under a new name, each marked on the row. */
  forks: SkillRow[];
  configurations: Configuration[];
  selected: boolean;
  onSelect: () => void;
}) => {
  const clients = row.agents
    .map((id) => configurations.find((c) => c.id === id))
    .filter((c) => c !== undefined);
  const shown = clients.slice(0, MAX_LOGOS);
  const description = [
    glyphTitle(row),
    ...row.pills.map((pill) => pill.label),
    ...forks.map((fork) => `Forked as ${fork.name}`),
    row.description,
  ]
    .filter((part) => part !== "")
    .join(". ");
  return (
    <ListRow
      label={row.name}
      description={description}
      selected={selected}
      onSelect={onSelect}
    >
      <span className="w-row-glyph flex h-5 flex-none items-center justify-center">
        <SkillGlyph
          shape={glyphOf(row)}
          lit={glyphLit(row)}
          title={glyphTitle(row)}
        />
      </span>
      <span className="gap-row-line flex min-w-0 flex-1 flex-col">
        <span className="gap-row-inline flex h-5 items-center">
          <span
            className={
              row.enabled
                ? "type-row-name text-row-name truncate"
                : "type-row-name text-row-name-off truncate"
            }
          >
            {row.name}
          </span>
          {row.pills.map((pill) => (
            <Pill key={pill.kind} tone={pillTones[pill.kind]}>
              {pill.label}
            </Pill>
          ))}
          {forks.map((fork) => (
            <SkillGlyph
              key={fork.key}
              shape="fork"
              lit={false}
              size={12}
              tone="caption"
              title={`Forked as ${fork.name}`}
            />
          ))}
          <span className="flex-1" />
          {shown.length > 0 && (
            <LogoStack
              clients={shown}
              size={20}
              extra={clients.length - shown.length}
            />
          )}
        </span>
        {row.description !== "" && (
          <span className="type-row-desc text-row-desc truncate">
            {row.description}
          </span>
        )}
      </span>
    </ListRow>
  );
};
