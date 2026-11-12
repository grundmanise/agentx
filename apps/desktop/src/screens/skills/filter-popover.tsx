import { useId, useState } from "react";
import type { ReactNode } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import {
  CheckOption,
  ChoiceGroup,
  ChoicePill,
  FilterButton,
  Segmented,
} from "#/components/filter-controls.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "#/components/ui/popover.tsx";
import { popularityOf } from "#/lib/agent-popularity.ts";
import {
  filterCount,
  GROUP_DISK,
  GROUP_PLUGINS,
  GROUP_YOURS,
  groupName,
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
  { label: groupName(GROUP_YOURS), value: "yours" },
  { label: groupName(GROUP_PLUGINS), value: "plugins" },
  { label: groupName(GROUP_DISK), value: "unmanaged" },
  { label: "Updates", value: "updates" },
];

const states: { value: State; label: string }[] = [
  { label: "All skills", value: "all" },
  { label: "Enabled", value: "enabled" },
  { label: "Disabled", value: "disabled" },
];

/** The names of the clients that read the library, for the Universal choice. */
export const universalNames = (universal: Configuration[]) =>
  universal.map((c) => c.name).join(", ");

/** The agent choices AGENTS shows before the rest is asked for. */
const AGENTS_SHOWN = 6;

/**
 * The choices of AGENTS: the checked ones, then the rest by the popularity of the agent client,
 * Universal where its most used client would stand. The order is fixed when the popover opens,
 * so a row stays in place when it is checked. A machine can have dozens of configurations, so
 * the list shows the first few and every choice that is checked, or was since the popover
 * opened, and a button shows the rest.
 */
const AgentChoices = ({
  configurations,
  checked,
  onToggle,
}: {
  configurations: Configuration[];
  /** The agents of the filters: configuration ids, and UNIVERSAL. */
  checked: string[];
  onToggle: (agent: string, checked: boolean) => void;
}) => {
  const universal = configurations.filter((c) => c.reads_library);
  const choices: {
    id: string;
    name: string;
    rank: number;
    label: ReactNode;
  }[] = configurations
    .filter((c) => !c.reads_library)
    .map((c) => ({
      id: c.id,
      label: (
        <>
          <AgentLogo client={c} size={20} decorative />
          <span className="type-secondary text-text-primary truncate">
            {c.name}
          </span>
        </>
      ),
      name: c.name,
      rank: popularityOf(c.id),
    }));
  if (universal.length > 0) {
    choices.push({
      id: UNIVERSAL,
      label: (
        <>
          <LogoStack clients={universal} size={20} decorative />
          <span className="flex min-w-0 flex-col">
            <span className="type-secondary text-text-primary">Universal</span>
            <span className="type-row-desc text-text-hint truncate">
              {universalNames(universal)}
            </span>
          </span>
        </>
      ),
      name: "Universal",
      rank: Math.min(...universal.map((c) => popularityOf(c.id))),
    });
  }
  choices.sort((a, b) => a.rank - b.rank || a.name.localeCompare(b.name));

  // The order of the popover as it opened: checked first. A configuration found since goes last.
  const [opened, setOpened] = useState(() =>
    [
      ...choices.filter((c) => checked.includes(c.id)),
      ...choices.filter((c) => !checked.includes(c.id)),
    ].map((c) => c.id)
  );
  const found = choices.filter((c) => !opened.includes(c.id));
  if (found.length > 0) {
    setOpened([...opened, ...found.map((c) => c.id)]);
  }
  choices.sort((a, b) => opened.indexOf(a.id) - opened.indexOf(b.id));

  // A choice checked since the popover opened stays in view, so unchecking it does not hide it.
  const [kept, setKept] = useState(() => new Set(checked));
  if (checked.some((id) => !kept.has(id))) {
    setKept(new Set([...kept, ...checked]));
  }

  const [expanded, setExpanded] = useState(false);
  // A button that would show one more row takes the room of that row: show the row instead.
  const short = choices.length <= AGENTS_SHOWN + 1;
  const shown = choices.filter(
    (c, i) => short || expanded || i < AGENTS_SHOWN || kept.has(c.id)
  );
  const hidden = choices.length - shown.length;

  return (
    <div className="flex flex-col">
      {shown.map((c) => (
        <CheckOption
          key={c.id}
          checked={checked.includes(c.id)}
          onChange={(on) => {
            onToggle(c.id, on);
          }}
        >
          {c.label}
        </CheckOption>
      ))}
      {(expanded || hidden > 0) && !short && (
        <div className="flex pt-1">
          <Button
            variant="ghost"
            size="sm"
            aria-expanded={expanded}
            onClick={() => {
              setExpanded(!expanded);
            }}
          >
            {expanded ? "Show fewer" : `Show ${hidden} more`}
          </Button>
        </div>
      )}
    </div>
  );
};

/**
 * The Filter button and its popover: SHOW (one of: every skill, one of the fixed groups, or the
 * skills with an update), STATE (one of), AGENTS (any of, Universal standing for every client
 * that reads the library), and Reset filters while any is applied. It scrolls when the window has
 * no room for all of it.
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

  const toggleAgent = (agent: string, checked: boolean) => {
    onChange({
      ...filters,
      agents: checked
        ? [...filters.agents, agent]
        : filters.agents.filter((a) => a !== agent),
    });
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <FilterButton count={count} />
      </PopoverTrigger>
      <PopoverContent aria-label="Filters">
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
          <AgentChoices
            configurations={configurations}
            checked={filters.agents}
            onToggle={toggleAgent}
          />
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
      </PopoverContent>
    </Popover>
  );
};
