// The Choose agents dialog of the Add skills bar (DESIGN.md §6 "Agent picker"): every
// configuration that takes a placement, by name, with its path and a checkbox, a search over the
// names, and Clear · Done. A row toggles the same targets as the bar's logos.
import { useId, useState } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo } from "#/components/agent-logo.tsx";
import { SearchField } from "#/components/search-field.tsx";
import { Button } from "#/components/ui/button.tsx";
import { Checkbox } from "#/components/ui/checkbox.tsx";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import { plural } from "#/lib/plural.ts";

/** One configuration: its logo, name and path, and the checkbox; the whole row toggles it. */
const Row = ({
  configuration,
  checked,
  onToggle,
}: {
  configuration: Configuration;
  checked: boolean;
  onToggle: () => void;
}) => {
  const id = useId();
  return (
    <li>
      <label
        htmlFor={id}
        className="rounded-agent-picker-row hover:bg-agent-picker-row-hover has-focus-visible:outline-shell-focus-ring flex cursor-pointer items-center gap-3 px-3 py-2 has-focus-visible:outline-2 has-focus-visible:-outline-offset-2"
      >
        <AgentLogo
          client={configuration}
          size={28}
          hover="tooltip"
          decorative
        />
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="type-body text-text-primary truncate">
            {configuration.name}
          </span>
          <span className="type-technical-sm text-text-caption truncate">
            {configuration.path}
          </span>
        </span>
        <Checkbox
          id={id}
          aria-label={configuration.name}
          checked={checked}
          onCheckedChange={onToggle}
        />
      </label>
    </li>
  );
};

/** Choose agents: the full list of install targets, searchable by name. */
export const AgentPickerDialog = ({
  open,
  onOpenChange,
  configurations,
  targets,
  onToggleTarget,
  onClearTargets,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The configurations that take a placement. */
  configurations: Configuration[];
  /** The ids of the chosen targets. */
  targets: string[];
  onToggleTarget: (id: string) => void;
  onClearTargets: () => void;
}) => {
  const [query, setQuery] = useState("");
  const text = query.trim().toLowerCase();
  const rows = configurations.filter((c) =>
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
      <DialogContent aria-describedby={undefined}>
        <DialogHeader>
          <DialogTitle>Choose agents</DialogTitle>
        </DialogHeader>
        <SearchField
          label="Search agents"
          placeholder="Search agents"
          value={query}
          onChange={setQuery}
        />
        {rows.length === 0 ? (
          <p className="type-secondary text-text-muted m-0 py-2 text-center">
            No agents match
          </p>
        ) : (
          <ul
            aria-label="Agents"
            className="max-h-agent-picker-list m-0 flex list-none flex-col overflow-auto p-0"
          >
            {rows.map((c) => (
              <Row
                key={c.id}
                configuration={c}
                checked={targets.includes(c.id)}
                onToggle={() => {
                  onToggleTarget(c.id);
                }}
              />
            ))}
          </ul>
        )}
        <DialogFooter>
          <output className="type-secondary text-text-muted flex min-w-0 flex-1 items-center">
            {plural(targets.length, "agent")}
          </output>
          <Button
            variant="ghost"
            disabled={targets.length === 0}
            onClick={onClearTargets}
          >
            Clear
          </Button>
          <Button
            variant="primary"
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
