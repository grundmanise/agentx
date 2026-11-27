// The agent picker of the Add skills bar (DESIGN.md §6 "Agent picker"), "Where to install": a
// search over the names, then every configuration that takes a placement and every universal one,
// by name. A row has its logo, name and path; a direct one has a checkbox that toggles the same
// targets as the bar's logos, a universal one the Universal pill and the lock. Then Clear · Done.
import { useId, useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo } from "#/components/agent-logo.tsx";
import { LockMark } from "#/components/check-mark.tsx";
import { HelpTerm } from "#/components/help-term.tsx";
import { SearchIcon } from "#/components/icons/search.tsx";
import { BadgeButton } from "#/components/ui/badge.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Checkbox } from "#/components/ui/checkbox.tsx";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "#/components/ui/input-group.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";

/** What universal agents are, in the tooltip of the subtitle's term and of the Universal pill. */
const universalTip = (
  <>
    Universal agents read your skills library,{" "}
    <span className="type-technical-sm">~/.agents/skills</span>, so every skill
    you add reaches them.
  </>
);

/** The name over the path, beside the logo. */
const Name = ({
  configuration,
  universal,
}: {
  configuration: Configuration;
  universal: boolean;
}) => (
  <span className="flex min-w-0 flex-1 flex-col gap-0.5">
    <span className="flex min-w-0 items-center gap-2">
      <span className="type-body text-text-primary truncate">
        {configuration.name}
      </span>
      {universal && (
        <Tooltip>
          <TooltipTrigger asChild>
            <BadgeButton tone="universal" help>
              Universal
            </BadgeButton>
          </TooltipTrigger>
          <TooltipContent size="text">{universalTip}</TooltipContent>
        </Tooltip>
      )}
    </span>
    <span className="type-technical-sm text-text-caption truncate">
      {configuration.path}
    </span>
  </span>
);

/**
 * A configuration that takes a placement, with the checkbox; the whole row toggles it. The agent
 * picker and the Add skill dialog's agent step list them.
 */
export const AgentRow = ({
  configuration,
  checked,
  onToggle,
  disabled = false,
}: {
  configuration: Configuration;
  checked: boolean;
  onToggle: () => void;
  /** The install runs: the choice is fixed. */
  disabled?: boolean;
}) => {
  const id = useId();
  return (
    <li>
      <label
        htmlFor={id}
        className="rounded-agent-picker-row hover:bg-agent-picker-row-hover has-focus-visible:outline-shell-focus-ring flex cursor-pointer items-center gap-3 px-3 py-2 has-focus-visible:outline-2 has-focus-visible:-outline-offset-2"
      >
        <AgentLogo client={configuration} size={28} decorative />
        <Name configuration={configuration} universal={false} />
        <Checkbox
          id={id}
          track="agent-picker-dialog.row-select"
          aria-label={configuration.name}
          checked={checked}
          disabled={disabled}
          onCheckedChange={onToggle}
        />
      </label>
    </li>
  );
};

/**
 * A universal configuration: its client reads the library, so every skill reaches it. The lock
 * stands where a row has its checkbox, and the row is no control.
 */
const UniversalRow = ({ configuration }: { configuration: Configuration }) => (
  <li className="flex items-center gap-3 px-3 py-2">
    <AgentLogo client={configuration} size={28} decorative />
    <Name configuration={configuration} universal />
    <LockMark />
    <span className="sr-only">Always on</span>
  </li>
);

/** The agent picker: every install target and every universal agent, searchable by name. */
export const AgentPickerDialog = ({
  open,
  onOpenChange,
  skills,
  configurations,
  universal,
  targets,
  onToggleTarget,
  onClearTargets,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** How many skills the install adds, for the subtitle's wording. */
  skills: number;
  /** The configurations that take a placement. */
  configurations: Configuration[];
  /** The configurations whose client reads the library. */
  universal: Configuration[];
  /** The ids of the chosen targets. */
  targets: string[];
  onToggleTarget: (id: string) => void;
  onClearTargets: () => void;
}) => {
  const [query, setQuery] = useState("");
  const one = skills === 1;
  const text = query.trim().toLowerCase();
  const universalIds = new Set(universal.map((c) => c.id));
  const rows = [...configurations, ...universal].filter((c) =>
    c.name.toLowerCase().includes(text)
  );
  rows.sort((a, b) => a.name.localeCompare(b.name));
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) {
          setQuery("");
        }
        onOpenChange(next);
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Where to install</DialogTitle>
          <DialogDescription>
            {`Choose agents to add the ${one ? "skill" : "skills"} to.`}
            {universal.length > 0 && (
              <>
                {` ${one ? "It is" : "They are"} always available to `}
                <HelpTerm
                  tip={universalTip}
                  trailing="."
                  track="agent-picker-dialog.universal-help"
                >
                  universal agents
                </HelpTerm>
              </>
            )}
          </DialogDescription>
        </DialogHeader>
        <InputGroup>
          <InputGroupAddon>
            <SearchIcon size="xs" />
          </InputGroupAddon>
          <InputGroupInput
            autoFocus
            kind="list-search"
            type="search"
            aria-label="Search agents"
            placeholder="Search agents"
            value={query}
            onChange={(e) => {
              setQuery(e.target.value);
            }}
          />
        </InputGroup>
        {rows.length === 0 ? (
          <p className="type-secondary text-text-muted m-0 py-2 text-center">
            No agents match
          </p>
        ) : (
          <ul
            aria-label="Agents"
            className="max-h-agent-picker-list m-0 flex list-none flex-col overflow-auto p-0"
          >
            {rows.map((c) =>
              universalIds.has(c.id) ? (
                <UniversalRow key={c.id} configuration={c} />
              ) : (
                <AgentRow
                  key={c.id}
                  configuration={c}
                  checked={targets.includes(c.id)}
                  onToggle={() => {
                    onToggleTarget(c.id);
                  }}
                />
              )
            )}
          </ul>
        )}
        <DialogFooter>
          <Button
            variant="ghost"
            track="agent-picker-dialog.clear"
            disabled={targets.length === 0}
            onClick={onClearTargets}
          >
            Clear
          </Button>
          <Button
            variant="primary"
            track="agent-picker-dialog.done"
            onClick={() => {
              setQuery("");
              onOpenChange(false);
            }}
          >
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
