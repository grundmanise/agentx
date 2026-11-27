// The Add skill dialog, which Add in the Skills header and the palette open: a Git link to a
// repository, a folder of skills or one skill. Find skills runs `source skills <link>`, a read that
// lists the skills under the link, from the network for a source not added and from the last fetch
// of one added, and changes nothing; the user checks the ones to install and the agents to install
// them in, and Install runs one `skill add <link> --name <a> --name <b> [--to <configuration>]...`
// (docs/help-center/cli/skill.mdx), which adds the source and installs them. Cancel after a lookup
// runs nothing. The first skill installed is selected once the inventory that follows lists it; a
// refusal of either step shows under the field.
import { useId, useRef, useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type {
  CliEvent,
  LibraryEntry,
  SourceSkillEvent,
} from "#/cli/events.gen.ts";
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
import {
  AgentTargets,
  useAgentTargets,
} from "#/screens/sources/agent-targets.tsx";
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
import { afterSettled } from "#/store/settle.ts";
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

/** What the list says when nothing under the link can be installed, or undefined when some can. */
const nothingToInstall = (cards: SkillCard[]): string | undefined => {
  if (cards.length === 0) {
    return "No skills under this link";
  }
  if (cards.every((c) => c.status === "added")) {
    return "Every skill under this link is already installed";
  }
  return cards.some((c) => c.status === undefined)
    ? undefined
    : "None of the skills under this link can be installed";
};

/** Whether the refusal is the field's to show: not a lock, whose toast has Retry, nor a cancel. */
const showsUnderField = (outcome: Outcome) =>
  outcome.exitCode !== EXIT_CODES.locked && !outcome.cancelled;

/**
 * The field, Find skills, the skills found and the agents, and the buttons. Mounted while the
 * dialog is open.
 */
const AddSkillForm = ({ onClose }: { onClose: () => void }) => {
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
  const finding = usePending(FIND_SKILLS_LABEL);
  // Install is pending while `skill add` runs and, once it installed the skills, until the inventory
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
  const oneEach = oneOfEachName(selectable);
  const selected = selectable.filter((c) => selection.has(c.key));
  const nothing = nothingToInstall(cards);

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

  const toggle = (card: SkillCard) => {
    setSelection((prev) => {
      const next = new Set(prev);
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
            {nothing === undefined && cards.length > 1 && (
              // Inset as the rows are, so its checkbox heads their column.
              <div className="px-3">
                <SelectAllRow
                  track="add-skill-dialog.select-all"
                  selected={selected.length}
                  total={oneEach.length}
                  onSelectAll={() => {
                    setSelection(
                      new Set(
                        [
                          ...selected,
                          ...oneOfEachName(selectable, selected),
                        ].map((c) => c.key)
                      )
                    );
                  }}
                  onClear={() => {
                    setSelection(new Set());
                  }}
                />
              </div>
            )}
            {nothing !== undefined && (
              <p className="type-secondary text-text-hint m-0">{nothing}</p>
            )}
            {cards.length > 0 && (
              <SkillRows
                cards={cards}
                selected={selection}
                onToggle={(card) => {
                  toggle(card);
                }}
              />
            )}
          </div>
          {nothing === undefined && (
            <div className="flex items-center gap-3">
              <AgentTargets
                skills={selected.length}
                direct={direct}
                universal={universal}
                targets={chosen}
                onToggleTarget={toggleTarget}
                onClearTargets={clearTargets}
                disabled={adding}
              />
            </div>
          )}
        </>
      )}
      <DialogFooter>
        <DialogClose asChild>
          <Button track="add-skill-dialog.cancel" variant="ghost">
            Cancel
          </Button>
        </DialogClose>
        <Button
          track="add-skill-dialog.install"
          variant="primary"
          disabled={found === undefined || selected.length === 0 || finding}
          aria-busy={adding ? true : undefined}
          onClick={() => {
            void install();
          }}
        >
          {adding && <Spinner />}
          Install
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
}) => (
  <Dialog
    open={open}
    onOpenChange={stopOnClose(
      [FIND_SKILLS_LABEL, ADD_SKILL_LABEL],
      onOpenChange
    )}
  >
    <DialogContent>
      <DialogHeader>
        <DialogTitle>Add skill</DialogTitle>
        <DialogDescription>
          Installs the skills you pick in your agents, and adds their source.
        </DialogDescription>
      </DialogHeader>
      <AddSkillForm
        onClose={() => {
          onOpenChange(false);
        }}
      />
    </DialogContent>
  </Dialog>
);
