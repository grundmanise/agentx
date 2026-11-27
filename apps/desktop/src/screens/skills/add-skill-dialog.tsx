// The Add skill dialog, which Add in the Skills header and the palette open, in two steps. First a
// Git link to a repository, a folder of skills or one skill: Find skills runs `source skills
// <link>`, a read that lists the skills under the link, from the network for a source not added
// and from the last fetch of one added, and changes nothing; the user checks the ones to install,
// searching them by name and description. Next shows the agents to install them in, and Install
// runs one `skill add <link> --name <a> --name <b> [--to <configuration>]...`
// (docs/help-center/cli/skill.mdx), which adds the source and installs them. Cancel after a lookup
// runs nothing. The first skill installed is selected once the inventory that follows lists it; a
// refusal shows under the field, or under the agents for Install.
import { useId, useRef, useState } from "react";
import type { ReactNode } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  Configuration,
  LibraryEntry,
  SourceSkillEvent,
} from "#/cli/events.gen.ts";
import { LogoStack } from "#/components/agent-logo.tsx";
import { LockMark } from "#/components/check-mark.tsx";
import { SearchIcon } from "#/components/icons/search.tsx";
import { ProblemText } from "#/components/problem-text.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Dialog,
  DialogClose,
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
import { Input } from "#/components/ui/input.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";
import { doctorRule } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import { openSkill } from "#/screens/skills/open.ts";
import { shortSource } from "#/screens/skills/projection.ts";
import {
  addSourceHintRules,
  linkProblem,
} from "#/screens/sources/add-source.ts";
import { AgentRow } from "#/screens/sources/agent-picker-dialog.tsx";
import { useAgentTargets } from "#/screens/sources/agent-targets.tsx";
import {
  count,
  linkAdd,
  listingCards,
  oneOfEachName,
  orderCards,
  sameName,
} from "#/screens/sources/projection.ts";
import type { SkillCard } from "#/screens/sources/projection.ts";
import { SelectAllRow } from "#/screens/sources/select-all-row.tsx";
import { SkillRows } from "#/screens/sources/skill-rows.tsx";
import { agentx, stopOnClose, usePending } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import type { Problem } from "#/store/store.ts";
import { useAppStore } from "#/store/store.ts";

/** The pending label of `skill add` from this dialog. */
export const ADD_SKILL_LABEL = "add skill";
/** The pending label of the lookup, `source skills`, from this dialog. */
export const FIND_SKILLS_LABEL = "find skills";

/**
 * The hints of `source skills` and `skill add` that name a command, in the app's words: the
 * hub's skills are under Hub, and Sources shows what a source holds.
 */
const linkHintRules: HintRule[] = [
  ...addSourceHintRules,
  {
    app: () => "see what the source holds under Sources",
    cli: /^run 'agentx source skills[^']*' to see what (?:it|the source) holds$/u,
  },
  doctorRule,
];

const noEntries: LibraryEntry[] = [];

/**
 * What Find skills found under a link: the link as typed, its source's canonical URL and the
 * listing. A source not added has no id, so installed skills match by URL and subpath.
 */
interface Found {
  link: string;
  url: string;
  listing: SourceSkillEvent[];
}

/** The names of the skills the run installed, from its `library_skill` events. */
const installedNames = (events: CliEvent[]) =>
  events.flatMap((e) => (e.type === "library_skill" ? [e.name] : []));

/** The success toast: the skills the run installed, as its events name them, and their source. */
const installedText = (events: CliEvent[], url: string) => {
  const names = installedNames(events);
  const what =
    names.length === 1 ? `${names[0]} skill` : count(names.length, "skill");
  return `Installed ${what} from ${shortSource(url)}`;
};

/**
 * What the list says when nothing under the link can be installed, or undefined when some can or
 * every one is installed already, which the rows say.
 */
const nothingToInstall = (cards: SkillCard[]): string | undefined => {
  if (cards.length === 0) {
    return "No skills under this link";
  }
  return cards.some((c) => c.status === undefined) ||
    cards.every((c) => c.status === "added")
    ? undefined
    : "None of the skills under this link can be installed";
};

/** Whether the search, in lower case, finds the skill by its name or description. */
const matches = (card: SkillCard, text: string) =>
  card.name.toLowerCase().includes(text) ||
  card.description.toLowerCase().includes(text);

/** A search field over a list, with the search icon at its left. */
const SearchField = ({
  label,
  placeholder,
  value,
  onChange,
  autoFocus = false,
}: {
  label: string;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
  autoFocus?: boolean;
}) => (
  <InputGroup>
    <InputGroupAddon>
      <SearchIcon size="xs" />
    </InputGroupAddon>
    <InputGroupInput
      autoFocus={autoFocus}
      kind="list-search"
      type="search"
      aria-label={label}
      placeholder={placeholder}
      value={value}
      onChange={(e) => {
        onChange(e.target.value);
      }}
    />
  </InputGroup>
);

/**
 * The universal agents, pinned under the agent list: they read the library, so every skill
 * reaches them. Three logos and "+n", the lock where a row has its checkbox. No control.
 */
const UniversalAgents = ({ universal }: { universal: Configuration[] }) => (
  <div className="border-shell-divider flex items-center gap-3 border-t px-3 pt-3">
    <LogoStack
      clients={universal.slice(0, 3)}
      hidden={universal.slice(3)}
      size={28}
    />
    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
      <span className="type-body text-text-primary">Universal agents</span>
      <span className="type-secondary text-text-muted">Always included</span>
    </span>
    <LockMark />
    <span className="sr-only">Always on</span>
  </div>
);

/** Whether the refusal is the field's to show: not a lock, whose toast has Retry, nor a cancel. */
const showsUnderField = (outcome: Outcome) =>
  outcome.exitCode !== EXIT_CODES.locked && !outcome.cancelled;

/**
 * The skills found: the select-all row and the search when several can be picked, then the rows.
 * The search acts on the rows it shows: Select all checks the matches, and a check it hides stays.
 */
const FoundSkills = ({
  cards,
  selection,
  onSelectionChange,
}: {
  cards: SkillCard[];
  /** The keys of the selected cards. */
  selection: ReadonlySet<string>;
  onSelectionChange: (selection: ReadonlySet<string>) => void;
}) => {
  const [query, setQuery] = useState("");
  const text = query.trim().toLowerCase();
  const shown = cards.filter((c) => matches(c, text));
  const shownSelectable = shown.filter((c) => c.status === undefined);
  const nothing = nothingToInstall(cards);
  const pickable = cards.filter((c) => c.status === undefined).length;
  const toggle = (card: SkillCard) => {
    const next = new Set(selection);
    if (next.has(card.key)) {
      next.delete(card.key);
    } else {
      // The library holds one skill of a name: selecting one deselects the other.
      for (const other of cards) {
        if (sameName(other, card)) {
          next.delete(other.key);
        }
      }
      next.add(card.key);
    }
    onSelectionChange(next);
  };
  return (
    <div className="flex flex-col gap-3">
      {pickable > 0 && cards.length > 1 && (
        // Inset as the rows are, so its checkbox heads their column.
        <div className="flex items-center justify-between gap-3 px-3">
          <SelectAllRow
            track="add-skill-dialog.select-all"
            selected={
              shownSelectable.filter((c) => selection.has(c.key)).length
            }
            total={oneOfEachName(shownSelectable).length}
            onSelectAll={() => {
              const taken = cards.filter((c) => selection.has(c.key));
              onSelectionChange(
                new Set([
                  ...selection,
                  ...oneOfEachName(shownSelectable, taken).map((c) => c.key),
                ])
              );
            }}
            onClear={() => {
              const next = new Set(selection);
              for (const c of shownSelectable) {
                next.delete(c.key);
              }
              onSelectionChange(next);
            }}
          />
          <div className="w-add-skill-search">
            <SearchField
              label="Search skills"
              placeholder="Search skills"
              value={query}
              onChange={setQuery}
            />
          </div>
        </div>
      )}
      {nothing !== undefined && (
        <p className="type-secondary text-text-hint m-0">{nothing}</p>
      )}
      {cards.length > 0 && shown.length === 0 && (
        <p className="type-secondary text-text-muted m-0 py-2 text-center">
          No skills match
        </p>
      )}
      {shown.length > 0 && (
        // A new search starts the list at its top.
        <SkillRows
          key={text}
          cards={shown}
          selected={selection}
          onToggle={(card) => {
            toggle(card);
          }}
        />
      )}
    </div>
  );
};

/**
 * The second step: a search over the agents that take a placement, its select-all row and their
 * rows by name, then the universal agents pinned under them, then a refusal of Install.
 */
const AgentsStep = ({
  direct,
  universal,
  chosen,
  onChoose,
  disabled,
  children,
}: {
  /** The configurations that take a placement. */
  direct: Configuration[];
  /** The configurations whose client reads the library. */
  universal: Configuration[];
  /** The ids of the chosen targets. */
  chosen: string[];
  onChoose: (ids: string[]) => void;
  /** The install runs: the choice is fixed. */
  disabled: boolean;
  children: ReactNode;
}) => {
  const [query, setQuery] = useState("");
  const text = query.trim().toLowerCase();
  const shown = direct.filter((c) => c.name.toLowerCase().includes(text));
  shown.sort((a, b) => a.name.localeCompare(b.name));
  const shownIds = shown.map((c) => c.id);
  return (
    <div className="flex flex-col gap-3">
      <SearchField
        autoFocus
        label="Search agents"
        placeholder={`Search ${direct.length} agents`}
        value={query}
        onChange={setQuery}
      />
      {shown.length > 0 && (
        <div className="px-3">
          <SelectAllRow
            track="add-skill-dialog.select-all-agents"
            selected={shownIds.filter((id) => chosen.includes(id)).length}
            total={shown.length}
            onSelectAll={() => {
              onChoose([...new Set([...chosen, ...shownIds])]);
            }}
            onClear={() => {
              onChoose(chosen.filter((id) => !shownIds.includes(id)));
            }}
          />
        </div>
      )}
      <div className="flex flex-col">
        {shown.length === 0 ? (
          <p className="type-secondary text-text-muted m-0 py-2 text-center">
            No agents match
          </p>
        ) : (
          <ul
            aria-label="Agents"
            className="max-h-add-skill-list m-0 flex list-none flex-col overflow-auto p-0"
          >
            {shown.map((c) => (
              <AgentRow
                key={c.id}
                configuration={c}
                checked={chosen.includes(c.id)}
                disabled={disabled}
                onToggle={() => {
                  onChoose(
                    chosen.includes(c.id)
                      ? chosen.filter((id) => id !== c.id)
                      : [...chosen, c.id]
                  );
                }}
              />
            ))}
          </ul>
        )}
        {universal.length > 0 && <UniversalAgents universal={universal} />}
      </div>
      {children}
    </div>
  );
};

/**
 * The dialog's two steps: the field, Find skills and the skills found, then the agents. Mounted
 * while the dialog is open.
 */
const AddSkillForm = ({ onClose }: { onClose: () => void }) => {
  const library = useAppStore((s) => s.inventory?.library) ?? noEntries;
  const { chosen, choose, direct, enabled, universal } = useAgentTargets();
  const [step, setStep] = useState<"skills" | "agents">("skills");
  const [link, setLink] = useState("");
  // The field as it stands, for a lookup that ends after the link changed.
  const current = useRef("");
  const [problem, setProblem] = useState<Problem | undefined>();
  // What was typed when it was refused, and the skills named: each keeps its case when it starts
  // the CLI's words.
  const [refused, setRefused] = useState<string[]>([]);
  const [found, setFound] = useState<Found | undefined>();
  const [selection, setSelection] = useState<ReadonlySet<string>>(new Set());
  const finding = usePending(FIND_SKILLS_LABEL);
  const adding = usePending(ADD_SKILL_LABEL);
  const hintId = useId();

  const cards =
    found === undefined
      ? []
      : orderCards(listingCards(found.listing, found.url, library));
  const selectable = cards.filter((c) => c.status === undefined);
  const selected = selectable.filter((c) => selection.has(c.key));

  const refuse = (
    outcome: Outcome,
    command: string,
    typed: string,
    names: string[] = []
  ) => {
    setProblem(linkProblem(outcome, { command, rules: linkHintRules, typed }));
    setRefused([typed, ...names]);
  };

  const lookUp = async (typed: string) => {
    if (typed === "" || finding || adding) {
      return;
    }
    setProblem(undefined);
    setFound(undefined);
    setSelection(new Set());
    const listed = await agentx(["source", "skills", typed], {
      label: FIND_SKILLS_LABEL,
      ownFailure: true,
      retry: () => {
        void lookUp(typed);
      },
      sideEffect: false,
    });
    // The link changed while it was looked up: what was found is not under it.
    if (current.current.trim() !== typed) {
      return;
    }
    if (listed.exitCode !== EXIT_CODES.ok) {
      if (showsUnderField(listed)) {
        refuse(listed, "source skills", typed);
      }
      return;
    }
    const url = listed.events.find((e) => e.type === "source")?.url ?? typed;
    const listing = listed.events.filter((e) => e.type === "source_skill");
    setFound({ link: typed, listing, url });
    // One skill found is checked; of several, the user picks.
    const [only, ...others] = listingCards(
      listing,
      url,
      useAppStore.getState().inventory?.library ?? noEntries
    );
    if (
      only !== undefined &&
      others.length === 0 &&
      only.status === undefined
    ) {
      setSelection(new Set([only.key]));
    }
  };

  const install = async () => {
    if (
      found === undefined ||
      selected.length === 0 ||
      adding ||
      ADD_SKILL_LABEL in useAppStore.getState().pending
    ) {
      return;
    }
    setProblem(undefined);
    const plan = linkAdd({
      enabled,
      link: found.link,
      selected,
      targets: chosen,
    });
    const outcome = await agentx(plan.args, {
      label: ADD_SKILL_LABEL,
      ownFailure: true,
      success: (events) => installedText(events, found.url),
    });
    if (outcome.exitCode !== EXIT_CODES.ok) {
      if (showsUnderField(outcome)) {
        refuse(outcome, "skill add", found.link, plan.names);
      }
      return;
    }
    const [first] = installedNames(outcome.events);
    onClose();
    if (first !== undefined) {
      openSkill(first);
    }
  };

  const refusal = problem !== undefined && (
    <ProblemText problem={problem} keep={refused} />
  );

  const skillsStep = (
    <>
      <div className="gap-input-label flex flex-col">
        <div className="flex items-center gap-2">
          <Input
            autoFocus
            kind="mono"
            aria-label="Link to skills"
            placeholder="Link to skills"
            aria-describedby={hintId}
            aria-invalid={problem === undefined ? undefined : true}
            value={link}
            onChange={(e) => {
              current.current = e.target.value;
              setLink(e.target.value);
              setProblem(undefined);
              // A new link is looked up again: what was found is under the old one.
              setFound(undefined);
              setSelection(new Set());
            }}
          />
          {found === undefined && (
            <Button
              track="add-skill-dialog.find"
              type="submit"
              variant="secondary"
              disabled={link.trim() === "" || adding}
              aria-busy={finding ? true : undefined}
            >
              {finding && <Spinner />}
              Find skills
            </Button>
          )}
        </div>
        <p id={hintId} className="type-secondary text-text-hint m-0">
          A GitHub or GitLab link to a repository, a folder or a skill
        </p>
        {refusal}
      </div>
      {found !== undefined && (
        <FoundSkills
          cards={cards}
          selection={selection}
          onSelectionChange={setSelection}
        />
      )}
    </>
  );

  const one = selected.length === 1;
  return (
    <form
      className="gap-dialog flex flex-col"
      onSubmit={(e) => {
        e.preventDefault();
        if (step === "skills" && found === undefined) {
          void lookUp(link.trim());
        }
      }}
    >
      <DialogHeader>
        <DialogTitle>
          {step === "skills" ? "Add skill" : "Where to install"}
        </DialogTitle>
        <DialogDescription>
          {step === "skills"
            ? "Find skills and install into your agents."
            : `Choose the agents that get ${one ? "this skill" : "these skills"}.`}
        </DialogDescription>
      </DialogHeader>
      {step === "skills" ? (
        skillsStep
      ) : (
        <AgentsStep
          direct={direct}
          universal={universal}
          chosen={chosen}
          onChoose={choose}
          disabled={adding}
        >
          {refusal}
        </AgentsStep>
      )}
      <DialogFooter>
        {step === "skills" ? (
          <>
            <DialogClose asChild>
              <Button track="add-skill-dialog.cancel" variant="ghost">
                Cancel
              </Button>
            </DialogClose>
            <Button
              track="add-skill-dialog.next"
              variant="primary"
              disabled={selected.length === 0 || finding}
              onClick={() => {
                setProblem(undefined);
                setStep("agents");
              }}
            >
              Next
            </Button>
          </>
        ) : (
          <>
            <Button
              track="add-skill-dialog.back"
              variant="ghost"
              disabled={adding}
              onClick={() => {
                setProblem(undefined);
                setStep("skills");
              }}
            >
              Back
            </Button>
            <Button
              track="add-skill-dialog.install"
              variant="primary"
              aria-busy={adding ? true : undefined}
              onClick={() => {
                void install();
              }}
            >
              {adding && <Spinner />}
              {`Install ${selected.length}`}
            </Button>
          </>
        )}
      </DialogFooter>
    </form>
  );
};

/**
 * The Add skill dialog. The form is mounted while the dialog is open, so it opens clean each time,
 * on its first step with the field focused.
 */
export const AddSkillDialog = ({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => (
  <Dialog
    open={open}
    onOpenChange={stopOnClose(
      [FIND_SKILLS_LABEL, ADD_SKILL_LABEL],
      onOpenChange
    )}
  >
    <DialogContent>
      <AddSkillForm
        onClose={() => {
          onOpenChange(false);
        }}
      />
    </DialogContent>
  </Dialog>
);
