// The Add skill dialog, which Add in the Skills header and the palette open: a Git link to a
// repository, a folder of skills or one skill. Find skills runs `source add <link>`, which adds
// the source or fetches it again, then `source skills <link>`, which lists the skills under the
// link; the user checks the ones to add and the agents to add them to, and Add runs one
// `skill add <link> --name <a> --name <b> [--to <configuration>]...` (docs/help-center/cli/skill.mdx).
// The first skill installed is selected once the inventory that follows lists it; a refusal of
// either step shows under the field.
import { useId, useRef, useState } from "react";
import type { RefObject } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  LibraryEntry,
  SourceSkillEvent,
} from "#/cli/events.gen.ts";
import { ProblemText } from "#/components/problem-text.tsx";
import { Spinner } from "#/components/spinner.tsx";
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
import { Input } from "#/components/ui/input.tsx";
import { doctorRule } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import { openSkill } from "#/screens/skills/open.ts";
import { shortSource } from "#/screens/skills/projection.ts";
import {
  addSourceHintRules,
  linkProblem,
} from "#/screens/sources/add-source-dialog.tsx";
import {
  AgentTargets,
  useAgentTargets,
} from "#/screens/sources/agent-targets.tsx";
import {
  count,
  linkAdd,
  listingCards,
  orderCards,
} from "#/screens/sources/projection.ts";
import { SelectAllRow } from "#/screens/sources/select-all-row.tsx";
import { SkillRows } from "#/screens/sources/skill-cards.tsx";
import { runCommand, usePending } from "#/store/runner.ts";
import type { Outcome } from "#/store/runner.ts";
import { afterSettled } from "#/store/settle.ts";
import type { Problem } from "#/store/store.ts";
import { useAppStore } from "#/store/store.ts";

/** The pending label of `skill add` from this dialog. */
export const ADD_SKILL_LABEL = "add skill";
/** The pending label of the lookup, `source add` then `source skills`, from this dialog. */
export const FIND_SKILLS_LABEL = "find skills";

/**
 * The hints of `source add`, `source skills` and `skill add` that name a command, in the app's
 * words: the account remote's skills are under Personal, and Sources shows what a source holds.
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

/** What Find skills found under a link: the link as typed, its source's URL and the listing. */
interface Found {
  link: string;
  url: string;
  listing: SourceSkillEvent[];
}

/** The names of the skills the run installed, from its `library_skill` events. */
const installedNames = (events: CliEvent[]) =>
  events.flatMap((e) => (e.type === "library_skill" ? [e.name] : []));

/** The success toast: the skills the run installed, as its events name them, and their source. */
const addedText = (events: CliEvent[], url: string) => {
  const names = installedNames(events);
  const what =
    names.length === 1 ? `${names[0]} skill` : count(names.length, "skill");
  return `Added ${what} from ${shortSource(url)}`;
};

/** Whether the refusal is the field's to show: not a lock, whose toast has Retry, nor a cancel. */
const showsUnderField = (outcome: Outcome) =>
  outcome.exitCode !== EXIT_CODES.locked && !outcome.cancelled;

/**
 * The field, Find skills, the skills found and the agents, and the buttons. Mounted while the
 * dialog is open.
 */
const AddSkillForm = ({
  linkField,
  onClose,
}: {
  linkField: RefObject<HTMLInputElement | null>;
  onClose: () => void;
}) => {
  const library = useAppStore((s) => s.inventory?.library) ?? noEntries;
  const {
    chosen,
    clear: clearTargets,
    direct,
    enabled,
    toggle: toggleTarget,
    universal,
  } = useAgentTargets();
  const [link, setLink] = useState("");
  // The field as it stands, for a lookup that ends after the link changed.
  const current = useRef("");
  const [problem, setProblem] = useState<Problem | undefined>();
  // What was typed when it was refused, and the skills named: each keeps its case when it starts
  // the CLI's words.
  const [refused, setRefused] = useState<string[]>([]);
  const [found, setFound] = useState<Found | undefined>();
  const [selection, setSelection] = useState<ReadonlySet<string>>(new Set());
  // Find skills is pending from `source add` through `source skills`.
  const [looking, setLooking] = useState(false);
  const finding = usePending(FIND_SKILLS_LABEL) || looking;
  // Add is pending while `skill add` runs and, once it installed the skills, until the inventory
  // that follows lists them. A refusal changed nothing, so it waits for no inventory.
  const running = usePending(ADD_SKILL_LABEL);
  const [selecting, setSelecting] = useState(false);
  const adding = running || selecting;
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
    setLooking(true);
    const retry = () => {
      void lookUp(typed);
    };
    const added = await runCommand(["source", "add", typed], {
      label: FIND_SKILLS_LABEL,
      ownFailure: true,
      retry,
    });
    const listed =
      added.exitCode === EXIT_CODES.ok
        ? await runCommand(["source", "skills", typed], {
            label: FIND_SKILLS_LABEL,
            ownFailure: true,
            retry,
          })
        : undefined;
    setLooking(false);
    // The link changed while it was looked up: what was found is not under it.
    if (current.current.trim() !== typed) {
      return;
    }
    if (listed === undefined || listed.exitCode !== EXIT_CODES.ok) {
      const outcome = listed ?? added;
      if (showsUnderField(outcome)) {
        refuse(
          outcome,
          listed === undefined ? "source add" : "source skills",
          typed
        );
      }
      return;
    }
    const url =
      [...listed.events, ...added.events].find((e) => e.type === "source")
        ?.url ?? typed;
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

  const add = async () => {
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
    const outcome = await runCommand(plan.args, {
      label: ADD_SKILL_LABEL,
      ownFailure: true,
      success: (events) => addedText(events, found.url),
    });
    if (outcome.exitCode !== EXIT_CODES.ok) {
      if (showsUnderField(outcome)) {
        refuse(outcome, "skill add", found.link, plan.names);
      }
      return;
    }
    // The skill is selected from the inventory that follows, never from the command's result.
    const [first] = installedNames(outcome.events);
    setSelecting(true);
    afterSettled(() => {
      onClose();
      if (first !== undefined) {
        openSkill(first);
      }
    });
  };

  const toggle = (key: string) => {
    setSelection((prev) => {
      const next = new Set(prev);
      if (next.has(key)) {
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  };

  return (
    <form
      className="gap-dialog flex flex-col"
      onSubmit={(e) => {
        e.preventDefault();
        void lookUp(link.trim());
      }}
    >
      <div className="gap-input-label flex flex-col">
        <div className="flex items-center gap-2">
          <Input
            ref={linkField}
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
          <Button
            type="submit"
            variant="secondary"
            disabled={link.trim() === "" || adding}
            aria-busy={finding ? true : undefined}
          >
            {finding && <Spinner />}
            Find skills
          </Button>
        </div>
        <p id={hintId} className="type-secondary text-text-hint m-0">
          A GitHub or GitLab link to a repository, a folder or a skill
        </p>
        {problem !== undefined && (
          <ProblemText problem={problem} keep={refused} />
        )}
      </div>
      {found !== undefined && (
        <>
          <div className="flex flex-col gap-3">
            {cards.length > 1 && (
              <SelectAllRow
                selected={selected.length}
                total={selectable.length}
                added={cards.length - selectable.length}
                onSelectAll={() => {
                  setSelection(new Set(selectable.map((c) => c.key)));
                }}
                onClear={() => {
                  setSelection(new Set());
                }}
              />
            )}
            {cards.length === 0 ? (
              <p className="type-secondary text-text-hint m-0">
                No skills under this link
              </p>
            ) : (
              <SkillRows
                cards={cards}
                selected={selection}
                onToggle={(card) => {
                  toggle(card.key);
                }}
              />
            )}
          </div>
          <div className="flex items-center gap-3">
            <AgentTargets
              direct={direct}
              universal={universal}
              targets={chosen}
              onToggleTarget={toggleTarget}
              onClearTargets={clearTargets}
              disabled={adding}
            />
          </div>
        </>
      )}
      <DialogFooter>
        <DialogClose asChild>
          <Button variant="ghost">Cancel</Button>
        </DialogClose>
        <Button
          variant="primary"
          disabled={found === undefined || selected.length === 0 || finding}
          aria-busy={adding ? true : undefined}
          onClick={() => {
            void add();
          }}
        >
          {adding && <Spinner />}
          Add
        </Button>
      </DialogFooter>
    </form>
  );
};

/**
 * The Add skill dialog. The form is mounted while the dialog is open, so it opens clean each time,
 * with the field focused.
 */
export const AddSkillDialog = ({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) => {
  const linkField = useRef<HTMLInputElement>(null);
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        onOpenAutoFocus={(e) => {
          e.preventDefault();
          linkField.current?.focus();
        }}
      >
        <DialogHeader>
          <DialogTitle>Add skill</DialogTitle>
          <DialogDescription>
            Adds skills from a link to your agents, and their source to Sources.
          </DialogDescription>
        </DialogHeader>
        <AddSkillForm
          linkField={linkField}
          onClose={() => {
            onOpenChange(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
};
