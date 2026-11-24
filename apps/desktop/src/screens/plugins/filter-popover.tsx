import { useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { ChoiceGroup, FilterButton } from "#/components/filter-controls.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "#/components/ui/popover.tsx";
import type { PluginFilters } from "#/screens/plugins/projection.ts";
import { AgentChoices } from "#/screens/skills/filter-popover.tsx";

/**
 * The Filter button of the Plugins list and its popover: AGENTS, any of the agent configurations
 * that hold a plugin, as the Skills list's popover offers them, and Reset filters while any is
 * applied.
 */
export const PluginFilterPopover = ({
  configurations,
  filters,
  onChange,
}: {
  /** The agent configurations that hold a plugin. */
  configurations: Configuration[];
  filters: PluginFilters;
  onChange: (filters: PluginFilters) => void;
}) => {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <FilterButton track="plugins.filter" count={filters.agents.length} />
      </PopoverTrigger>
      <PopoverContent aria-label="Filters">
        <ChoiceGroup caption="Agents">
          <AgentChoices
            configurations={configurations}
            universal={false}
            checked={filters.agents}
            onToggle={(agent, checked) => {
              onChange({
                ...filters,
                agents: checked
                  ? [...filters.agents, agent]
                  : filters.agents.filter((a) => a !== agent),
              });
            }}
          />
        </ChoiceGroup>
        {filters.agents.length > 0 && (
          <div className="flex">
            <Button
              variant="ghost"
              size="sm"
              track="plugins.filter-reset"
              onClick={() => {
                onChange({ ...filters, agents: [] });
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
