import { useState } from "react";

import { AgentLogo } from "#/components/agent-logo.tsx";
import { DetailFact, DetailFacts } from "#/components/detail-facts.tsx";
import { LIST_GUIDE } from "#/components/list-guide.ts";
import { SectionToggle } from "#/components/section-toggle.tsx";
import { Badge } from "#/components/ui/badge.tsx";
import {
  Collapsible,
  CollapsibleContent,
} from "#/components/ui/collapsible.tsx";
import { nameOf } from "#/screens/machines/machine-list.tsx";
import { changedInFull, osName } from "#/screens/machines/projection.ts";
import type {
  AgentGroup,
  MachineView,
  SkillLine,
} from "#/screens/machines/projection.ts";

/** What brings a skill to an agent, in the app's words. */
const fromText = (skill: SkillLine) => {
  if (skill.from === "library") {
    return "Library";
  }
  return skill.from === "plugin"
    ? `${skill.plugin ?? ""} plugin`
    : "Skills directory";
};

/** Where a library skill is published: the hub, with a fork's upstream, or its source. */
const sourceText = (skill: SkillLine) => {
  const { source } = skill;
  if (source === undefined) {
    return "";
  }
  if (!source.hub) {
    return source.short;
  }
  return source.short === "" ? "Hub" : `Hub, from ${source.short}`;
};

/** A skill's row inside its agent's group, beside the group's guide line. */
const ROW = `pl-subgroup-indent relative flex ${LIST_GUIDE}`;

/**
 * One skill an agent sees: its name and the Modified pill of a library skill edited on that
 * machine, then what brings it and, for a library skill, where it is published. It opens nothing.
 */
const SkillItem = ({ skill }: { skill: SkillLine }) => {
  const source = sourceText(skill);
  return (
    <li className={ROW}>
      <div className="flex min-w-0 flex-1 items-center gap-3 py-2 pr-3 pl-3.5">
        <span className="flex min-w-0 flex-1 items-center gap-2">
          <span className="type-row-name text-row-name truncate select-text">
            {skill.name}
          </span>
          {skill.modified && <Badge tone="neutral">Modified</Badge>}
        </span>
        <span className="flex min-w-0 flex-1 items-baseline gap-2">
          <span className="type-secondary text-text-muted whitespace-nowrap">
            {fromText(skill)}
          </span>
          {source !== "" && (
            <span
              title={skill.source?.full === "" ? undefined : skill.source?.full}
              className="type-technical-sm text-text-caption truncate"
            >
              {source}
            </span>
          )}
        </span>
      </div>
    </li>
  );
};

/**
 * One agent configuration of the machine: its header, and the skills its agent client sees. The
 * header is the trigger of the Collapsible that holds the group.
 */
const AgentSection = ({
  agent,
  first,
  open,
  onToggle,
}: {
  agent: AgentGroup;
  first: boolean;
  open: boolean;
  onToggle: () => void;
}) => (
  <Collapsible asChild open={open} onOpenChange={onToggle}>
    <section
      aria-label={agent.name}
      className={first ? "flex flex-col" : "pt-group-spacing flex flex-col"}
    >
      <h2 className="bg-shell-content-bg sticky top-0 z-10 m-0">
        <SectionToggle
          level="group"
          label={agent.name}
          icon={
            <AgentLogo
              client={{ client: agent.id, name: agent.name }}
              size={16}
              decorative
            />
          }
          count={agent.skills.length}
          mark={
            agent.enabled ? undefined : <Badge tone="disabled">Disabled</Badge>
          }
        />
      </h2>
      {agent.skills.length > 0 && (
        <CollapsibleContent asChild>
          <ul className="m-0 flex list-none flex-col p-0">
            {agent.skills.map((skill) => (
              <SkillItem key={skill.key} skill={skill} />
            ))}
          </ul>
        </CollapsibleContent>
      )}
    </section>
  </Collapsible>
);

/**
 * The open machine (DESIGN.md §6 "Machines screen"): its label, the This machine pill on this
 * machine, one row of facts, then the skills each of its agents sees, one group per agent
 * configuration. Only the groups scroll: the title and the facts stay put.
 */
export const MachineDetail = ({ machine }: { machine: MachineView }) => {
  // The agent configurations whose groups are collapsed; every group is open at first.
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(
    () => new Set<string>()
  );
  const toggle = (id: string) => {
    setCollapsed((was) => {
      const next = new Set(was);
      if (!next.delete(id)) {
        next.add(id);
      }
      return next;
    });
  };
  const name = nameOf(machine);
  return (
    <article
      aria-label={name}
      className="pt-detail-top px-detail pb-detail-bottom max-w-detail gap-detail-section flex h-full min-h-0 flex-col"
    >
      <header className="gap-detail-section flex flex-none flex-col">
        <div className="gap-detail-header flex flex-col">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="type-machine-name text-text-primary m-0 break-all select-text">
              {name}
            </h1>
            {machine.kind === "this" && (
              <Badge tone="neutral" size="detail">
                This machine
              </Badge>
            )}
          </div>
          {machine.kind === "cloud" && (
            <p className="type-detail-desc text-text-muted m-0 text-pretty">
              Each cloud session is given these skills.
            </p>
          )}
        </div>
        <DetailFacts>
          {machine.kind !== "cloud" && (
            <DetailFact label="Machine id">
              <span className="select-text">{machine.id}</span>
            </DetailFact>
          )}
          {machine.os !== undefined && (
            <DetailFact label="OS">{osName(machine.os)}</DetailFact>
          )}
          {machine.agentx !== undefined && (
            <DetailFact label="agentx">{machine.agentx}</DetailFact>
          )}
          {machine.changedAt !== undefined && (
            <DetailFact label="Last change">
              {changedInFull(machine.changedAt)}
            </DetailFact>
          )}
        </DetailFacts>
      </header>
      {machine.agents.length === 0 ? (
        <p className="type-secondary text-text-hint m-0">No agents found</p>
      ) : (
        <div className="-mx-3.5 min-h-0 flex-1 overflow-auto">
          {machine.agents.map((agent, at) => (
            <AgentSection
              key={agent.id}
              agent={agent}
              first={at === 0}
              open={!collapsed.has(agent.id)}
              onToggle={() => {
                toggle(agent.id);
              }}
            />
          ))}
        </div>
      )}
    </article>
  );
};
