import { useId, useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import {
  CheckOption,
  ChoiceGroup,
  ChoicePill,
  FilterButton,
  Segmented,
} from "#/components/filter-controls.tsx";
import { Popover } from "#/components/popover.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  filterCount,
  noFilters,
  showCounts,
  UNIVERSAL,
} from "#/screens/skills/projection.ts";
import type {
  Filters,
  Show,
  SkillRow,
  State,
} from "#/screens/skills/projection.ts";

const shows: { value: Show; label: string }[] = [
  { label: "All skills", value: "all" },
  { label: "Updates", value: "updates" },
  { label: "Yours", value: "yours" },
  { label: "Unmanaged", value: "unmanaged" },
];

const states: { value: State; label: string }[] = [
  { label: "All skills", value: "all" },
  { label: "Enabled", value: "enabled" },
  { label: "Disabled", value: "disabled" },
];

/** The names of the clients that read the library, for the Universal choice. */
export const universalNames = (universal: Configuration[]) =>
  universal.map((c) => c.name).join(", ");

/**
 * The Filter button and its popover: SHOW (one of), STATE (one of), AGENTS (any of, Universal
 * standing for every client that reads the library), and Reset filters while any is applied.
 */
export const FilterPopover = ({
  rows,
  configurations,
  filters,
  onChange,
}: {
  /** Every row, before the filters: the SHOW counts come from them. */
  rows: SkillRow[];
  configurations: Configuration[];
  filters: Filters;
  onChange: (filters: Filters) => void;
}) => {
  const [open, setOpen] = useState(false);
  const id = useId();
  const counts = showCounts(rows, filters);
  const count = filterCount(filters);
  const direct = configurations.filter((c) => !c.reads_library);
  const universal = configurations.filter((c) => c.reads_library);

  const toggleAgent = (agent: string, checked: boolean) => {
    onChange({
      ...filters,
      agents: checked
        ? [...filters.agents, agent]
        : filters.agents.filter((a) => a !== agent),
    });
  };

  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      label="Filters"
      trigger={<FilterButton count={count} />}
    >
      <ChoiceGroup caption="Show">
        <div className="flex flex-wrap gap-1">
          {shows.map((s) => (
            <ChoicePill
              key={s.value}
              group={`${id}-show`}
              active={filters.show === s.value}
              count={counts[s.value]}
              onSelect={() => {
                onChange({ ...filters, show: s.value });
              }}
            >
              {s.label}
            </ChoicePill>
          ))}
        </div>
      </ChoiceGroup>
      <ChoiceGroup caption="State">
        <Segmented
          group={`${id}-state`}
          options={states}
          value={filters.state}
          onChange={(state) => {
            onChange({ ...filters, state });
          }}
        />
      </ChoiceGroup>
      <ChoiceGroup caption="Agents">
        <div className="flex flex-col">
          {direct.map((c) => (
            <CheckOption
              key={c.id}
              checked={filters.agents.includes(c.id)}
              onChange={(checked) => {
                toggleAgent(c.id, checked);
              }}
            >
              <AgentLogo client={c} size={20} decorative />
              <span className="type-secondary text-text-primary truncate">
                {c.name}
              </span>
            </CheckOption>
          ))}
          {universal.length > 0 && (
            <CheckOption
              checked={filters.agents.includes(UNIVERSAL)}
              onChange={(checked) => {
                toggleAgent(UNIVERSAL, checked);
              }}
            >
              <LogoStack clients={universal} size={20} decorative />
              <span className="flex min-w-0 flex-col">
                <span className="type-secondary text-text-primary">
                  Universal
                </span>
                <span className="type-row-desc text-text-hint truncate">
                  {universalNames(universal)}
                </span>
              </span>
            </CheckOption>
          )}
        </div>
      </ChoiceGroup>
      {count > 0 && (
        <div className="flex">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              onChange({ ...noFilters, name: filters.name });
            }}
          >
            Reset filters
          </Button>
        </div>
      )}
    </Popover>
  );
};
