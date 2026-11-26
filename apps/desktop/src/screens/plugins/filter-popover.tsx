import { useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { ChoiceGroup, FilterButton } from "#/components/filter-controls.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "#/components/ui/popover.tsx";
import { AgentChoices } from "#/screens/skills/filter-popover.tsx";

/**
 * The Filter button of the Plugins list and of the MCP servers screen, and its popover: AGENTS,
 * any of the agent configurations the screen lists, as the Skills list's popover offers them, and
 * Reset filters while any is applied.
 */
export const AgentFilterPopover = ({
  configurations,
  agents,
  onChange,
  track,
}: {
  /** The agent configurations the screen lists. */
  configurations: Configuration[];
  /** The checked configuration ids. */
  agents: string[];
  onChange: (agents: string[]) => void;
  /** The screen's segment of the click names: "plugins" sends "plugins.filter". */
  track: string;
}) => {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <FilterButton track={`${track}.filter`} count={agents.length} />
      </PopoverTrigger>
      <PopoverContent aria-label="Filters">
        <ChoiceGroup caption="Agents">
          <AgentChoices
            configurations={configurations}
            universal={false}
            checked={agents}
            onToggle={(agent, checked) => {
              onChange(
                checked ? [...agents, agent] : agents.filter((a) => a !== agent)
              );
            }}
          />
        </ChoiceGroup>
        {agents.length > 0 && (
          <div className="flex">
            <Button
              variant="ghost"
              size="sm"
              track={`${track}.filter-reset`}
              onClick={() => {
                onChange([]);
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
