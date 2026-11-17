// What each agent sees, in the detail pane's AGENTS: an info button beside the caption, and, when
// some agent sees more than one skill of the name, a notice under the chips with Details. Both
// open the same popover: a group for each place that holds a skill of the name, with the agents
// that see it there (`sightsOf`).
import { useId } from "react";
import type { ReactElement } from "react";

import { AgentLogo } from "#/components/agent-logo.tsx";
import { InfoIcon } from "#/components/icons/info.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Popover,
  PopoverContent,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "#/components/ui/popover.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { crowdedText } from "#/screens/skills/sights.ts";
import type {
  NameSights,
  PlaceAgent,
  Through,
} from "#/screens/skills/sights.ts";

const TITLE = "What each agent sees";

const throughText: Record<Through, string> = {
  copy: "(copy)",
  link: "(through a link)",
};

const AgentItem = ({ agent }: { agent: PlaceAgent }) => (
  <li className="flex items-center gap-1.5">
    <AgentLogo client={agent.configuration} size={16} decorative />
    <span className="type-secondary text-text-secondary">
      {agent.configuration.name}
    </span>
    {agent.through !== undefined && (
      <span className="type-secondary text-text-hint">
        {throughText[agent.through]}
      </span>
    )}
  </li>
);

/** The popover's body: one group per place, then what happens with two skills of one name. */
const SightsBody = ({ sights, row }: { sights: NameSights; row: SkillRow }) => {
  const titleId = useId();
  const crowded = sights.crowded.length > 0;
  return (
    <PopoverContent aria-labelledby={titleId}>
      <PopoverHeader>
        <PopoverTitle id={titleId}>{TITLE}</PopoverTitle>
      </PopoverHeader>
      {sights.places.map((place) => (
        <section
          key={place.key}
          aria-label={place.heading}
          className="flex flex-col gap-2"
        >
          <h3 className="type-secondary text-text-muted m-0">
            {place.heading}
          </h3>
          <ul className="m-0 flex list-none flex-wrap gap-x-3 gap-y-2 p-0">
            {place.agents.map((agent) => (
              <AgentItem key={agent.configuration.id} agent={agent} />
            ))}
          </ul>
        </section>
      ))}
      {crowded && (
        <p className="type-secondary text-text-muted m-0">
          {row.kind === "plugin"
            ? "An agent that finds two skills with the same name decides which one it uses. To avoid this, fork it with a different name."
            : "An agent that finds two skills with the same name decides which one it uses."}
        </p>
      )}
    </PopoverContent>
  );
};

/** The popover, opened by `trigger`. */
const SightsPopover = ({
  sights,
  row,
  trigger,
}: {
  sights: NameSights;
  row: SkillRow;
  trigger: ReactElement;
}) => (
  <Popover>
    <PopoverTrigger asChild>{trigger}</PopoverTrigger>
    <SightsBody sights={sights} row={row} />
  </Popover>
);

/** The info button beside the AGENTS caption, while some agent sees the skill. */
export const SightsButton = ({
  sights,
  row,
}: {
  sights: NameSights;
  row: SkillRow;
}) => {
  if (sights.places.length === 0) {
    return null;
  }
  return (
    <Tooltip>
      <SightsPopover
        sights={sights}
        row={row}
        trigger={
          <TooltipTrigger asChild>
            <Button variant="quiet" size="icon-xs" aria-label={TITLE}>
              <InfoIcon size="xs" />
            </Button>
          </TooltipTrigger>
        }
      />
      <TooltipContent>{TITLE}</TooltipContent>
    </Tooltip>
  );
};

/** The line under the chips while some agent sees more than one skill of the name, with Details. */
export const SightsNotice = ({
  sights,
  row,
}: {
  sights: NameSights;
  row: SkillRow;
}) => {
  const text = crowdedText(sights);
  if (text === undefined) {
    return null;
  }
  return (
    <p className="type-secondary text-text-muted m-0">
      {`${text} `}
      <SightsPopover
        sights={sights}
        row={row}
        trigger={
          <Button variant="link" size="inline">
            Details
          </Button>
        }
      />
    </p>
  );
};
