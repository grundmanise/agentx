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
 * One skill in the list: glyph, name, pills and the logos of the agent clients that see it, then
 * its description. A nested row, a fork under a new name shown under its original, carries the
 * tag "your skill". Selecting it shows the skill in the detail pane.
 */
export const SkillRowItem = ({
  row,
  configurations,
  selected,
  nested = false,
  onSelect,
}: {
  row: SkillRow;
  configurations: Configuration[];
  selected: boolean;
  nested?: boolean;
  onSelect: () => void;
}) => {
  const clients = row.agents
    .map((id) => configurations.find((c) => c.id === id))
    .filter((c) => c !== undefined);
  const shown = clients.slice(0, MAX_LOGOS);
  return (
    <ListRow
      label={nested ? `${row.name}, your skill` : row.name}
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
          {nested && (
            <span className="type-technical-sm text-text-caption flex-none">
              your skill
            </span>
          )}
          {row.pills.map((pill) => (
            <Pill key={pill.kind} tone={pillTones[pill.kind]}>
              {pill.label}
            </Pill>
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
