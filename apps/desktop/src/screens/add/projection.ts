// What the Add skills screen shows, projected from the store and nothing else: the source pills
// from the Sources slice, the cards of a listing, of your skills on the account remote or of a
// search answer, each card's state from the inventory's library, and the `skill add` command
// lines a selection produces. Pure functions, so that every part of the screen reads a skill the
// same way and the tests pin the command lines.
import type {
  Configuration,
  InstallableSkillEvent,
  LibraryEntry,
  SearchResult,
  SourceEvent,
  SourceSkillEvent,
} from "#/cli/events.gen.ts";
import { formatAgo } from "#/lib/dates.ts";
import { shortSource } from "#/screens/skills/projection.ts";

/** The key of the Personal pill and the source of its cards, beside the source ids and URLs. */
export const YOUR_SKILLS = "your-skills";

/** The label of the Personal pill and of its cards' group. */
export const YOUR_SKILLS_LABEL = "Personal";

export interface SourcePill {
  /** The source id, or YOUR_SKILLS. */
  key: string;
  /** The source's short form, or "Personal". */
  label: string;
  /** The canonical URL; absent for Personal. */
  url?: string;
  lastFetched?: string;
}

const compareLabels = (a: string, b: string) =>
  a.localeCompare(b, undefined, { sensitivity: "base" });

/**
 * One pill per shared source, A to Z by short form, then Personal when an account remote is
 * set.
 */
export const sourcePills = (sources: SourceEvent[]): SourcePill[] => {
  const shared = sources
    .filter((s) => s.account !== true)
    .map((s): SourcePill => {
      const pill: SourcePill = {
        key: s.id,
        label: shortSource(s.url),
        url: s.url,
      };
      if (s.last_fetched !== undefined) {
        pill.lastFetched = s.last_fetched;
      }
      return pill;
    });
  shared.sort(
    (a, b) => compareLabels(a.label, b.label) || a.key.localeCompare(b.key)
  );
  const account = sources.find((s) => s.account === true);
  if (account === undefined) {
    return shared;
  }
  const yours: SourcePill = { key: YOUR_SKILLS, label: YOUR_SKILLS_LABEL };
  if (account.last_fetched !== undefined) {
    yours.lastFetched = account.last_fetched;
  }
  return [...shared, yours];
};

/** "1 skill", "2 skills". */
export const count = (n: number, noun: string) =>
  `${n} ${noun}${n === 1 ? "" : "s"}`;

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
/** A source not fetched for this long gets the attention dot. */
const STALE_AFTER = 2 * DAY;

const ageOf = (lastFetched: string, now: number): number | undefined => {
  const at = Date.parse(lastFetched);
  return Number.isNaN(at) ? undefined : Math.max(0, now - at);
};

/** The title of the attention dot on a stale pill; undefined when the source is fresh. */
export const staleTitle = (
  lastFetched: string | undefined,
  now: number
): string | undefined => {
  if (lastFetched === undefined) {
    return "Not fetched yet";
  }
  const age = ageOf(lastFetched, now);
  if (age === undefined || age < STALE_AFTER) {
    return undefined;
  }
  return `Not checked in ${count(Math.floor(age / DAY), "day")}`;
};

/** The label of the refresh button: when the source was last fetched. */
export const fetchedLabel = (
  lastFetched: string | undefined,
  now: number
): string => {
  const ago =
    lastFetched === undefined ? undefined : formatAgo(lastFetched, now);
  return ago === undefined ? "Not fetched yet" : `Checked ${ago}`;
};

/** Why a card cannot be selected, from the inventory's library. */
export type CardStatus = "added" | "forked" | "on-disk" | "name-in-use";

export const statusLabels: Record<CardStatus, string> = {
  added: "Added",
  forked: "Forked",
  "name-in-use": "Name in use",
  "on-disk": "Already in library",
};

export interface SkillCard {
  /** The card's own key: its source and name. */
  key: string;
  /** Where the card installs from: the source's canonical URL, or YOUR_SKILLS. */
  source: string;
  name: string;
  description: string;
  /** Why it cannot be selected; absent for a selectable card. */
  status?: CardStatus;
  /** The tooltip of a card that cannot be selected. */
  title?: string;
}

export const cardKey = (source: string, name: string) =>
  `${source}\u0000${name}`;

const card = (
  source: string,
  name: string,
  description: string,
  status: CardStatus | undefined
): SkillCard => {
  const c: SkillCard = {
    description,
    key: cardKey(source, name),
    name,
    source,
  };
  if (status !== undefined) {
    c.status = status;
    if (status === "name-in-use") {
      c.title = `Remove or rename ${name} first`;
    }
    if (status === "on-disk") {
      c.title = `A folder named ${name} is already in your library, added outside agentx. Adopt it in Skills to manage it, or remove it to add this one.`;
    }
  }
  return c;
};

/** The state of a skill of a shared source, by its source URL and subpath, then by its name. */
const sourceSkillStatus = (
  url: string,
  subpath: string,
  name: string,
  library: LibraryEntry[]
): CardStatus | undefined => {
  if (
    library.some(
      (e) => e.kind === "managed" && e.source === url && e.subpath === subpath
    )
  ) {
    return "added";
  }
  if (
    library.some(
      (e) => e.upstream === url && (e.upstream_subpath ?? "") === subpath
    )
  ) {
    return "forked";
  }
  const named = library.find((e) => e.name === name);
  if (named === undefined) {
    return undefined;
  }
  return named.kind === "unmanaged" ? "on-disk" : "name-in-use";
};

/** The cards of a source's listing, as `source skills <id>` gave it. */
export const listingCards = (
  listing: SourceSkillEvent[],
  url: string,
  library: LibraryEntry[]
): SkillCard[] =>
  listing.map((s) =>
    card(
      url,
      s.name,
      s.description,
      sourceSkillStatus(url, s.subpath, s.name, library)
    )
  );

/**
 * The state of one of your skills on the account remote, by its name: a library entry of the
 * name is in the way, unless it is the same skill installed from its upstream without edits,
 * which the install replaces.
 */
const installableStatus = (
  skill: InstallableSkillEvent,
  library: LibraryEntry[]
): CardStatus | undefined => {
  const named = library.find((e) => e.name === skill.name);
  if (named === undefined) {
    return undefined;
  }
  if (named.kind === "unmanaged") {
    return "on-disk";
  }
  const sameUpstream =
    skill.upstream !== undefined &&
    named.source === skill.upstream &&
    named.subpath === (skill.upstream_subpath ?? "");
  return sameUpstream && named.state === "current" ? undefined : "name-in-use";
};

/** The cards of your skills on the account remote, as `skill list --remote` gave them. */
export const installableCards = (
  installable: InstallableSkillEvent[],
  library: LibraryEntry[]
): SkillCard[] =>
  installable.map((s) =>
    card(
      YOUR_SKILLS,
      s.name,
      s.upstream === undefined
        ? "personal skill"
        : `from ${shortSource(s.upstream)}`,
      installableStatus(s, library)
    )
  );

/** The cards of a search answer, whose `source` is each skill's source URL. */
export const resultCards = (
  results: SearchResult[],
  library: LibraryEntry[]
): SkillCard[] =>
  results.map((r) =>
    card(
      r.source,
      r.name,
      r.description,
      sourceSkillStatus(r.source, r.subpath, r.name, library)
    )
  );

/** The cards in their order: selectable first, then by name. */
export const orderCards = (cards: SkillCard[]): SkillCard[] => {
  const ordered = [...cards];
  ordered.sort(
    (a, b) =>
      Number(a.status !== undefined) - Number(b.status !== undefined) ||
      compareLabels(a.name, b.name) ||
      a.key.localeCompare(b.key)
  );
  return ordered;
};

export interface CardGroup {
  /** The source URL, or YOUR_SKILLS. */
  source: string;
  /** The source's short form, or "Personal". */
  label: string;
  cards: SkillCard[];
}

const sourceLabel = (source: string) =>
  source === YOUR_SKILLS ? YOUR_SKILLS_LABEL : shortSource(source);

/** The cards grouped by source, the sources A to Z by label. */
export const groupCards = (cards: SkillCard[]): CardGroup[] => {
  const sources = [...new Set(cards.map((c) => c.source))];
  sources.sort(
    (a, b) =>
      compareLabels(sourceLabel(a), sourceLabel(b)) || a.localeCompare(b)
  );
  return sources.map((source) => ({
    cards: cards.filter((c) => c.source === source),
    label: sourceLabel(source),
    source,
  }));
};

/** The configurations whose client does not read the library: the install targets. */
export const directConfigurations = (configurations: Configuration[]) =>
  configurations.filter((c) => !c.reads_library);

/** The configurations whose client reads the library: the Universal stack. */
export const universalConfigurations = (configurations: Configuration[]) =>
  configurations.filter((c) => c.reads_library);

/** "Codex and Gemini CLI always get skills from your library". */
export const universalTitle = (names: string[]) => {
  const list =
    names.length <= 1
      ? names.join("")
      : `${names.slice(0, -1).join(", ")} and ${names.at(-1) ?? ""}`;
  return `${list} always get skills from your library`;
};

const sameSet = (a: string[], b: string[]) =>
  a.length === b.length && a.every((id) => b.includes(id));

export interface AddPlan {
  /** The command line, without --json. */
  args: string[];
  /** The source the command installs from: its URL, or YOUR_SKILLS. */
  source: string;
  /** The names the command installs. */
  names: string[];
}

export interface PlanInput {
  /** The selected cards. */
  selected: SkillCard[];
  /**
   * The listing the user started from with Select all or the preselection: that source's command
   * takes `--all` and one `--except` per card of the listing not selected.
   */
  allOf?: { source: string; cards: SkillCard[] };
  /** The chosen install targets, in inventory order. */
  targets: string[];
  /** The enabled configurations among the targets: `--to` is given only when the two differ. */
  enabled: string[];
}

/**
 * One `skill add` per source among the selected cards, sources A to Z by label
 * (docs/help-center/cli/skill.mdx, "Install several at once" and "Choose where it goes").
 */
export const planAdds = ({
  selected,
  allOf,
  targets,
  enabled,
}: PlanInput): AddPlan[] => {
  const to = sameSet(targets, enabled)
    ? []
    : targets.flatMap((id) => ["--to", id]);
  return groupCards(selected).map(({ source, cards: chosen }) => {
    const cards = orderCards(chosen);
    const names = cards.map((c) => c.name);
    const selection =
      allOf !== undefined && allOf.source === source
        ? [
            "--all",
            ...allOf.cards.flatMap((c) =>
              cards.some((s) => s.key === c.key) ? [] : ["--except", c.name]
            ),
          ]
        : names.flatMap((name) => ["--name", name]);
    return {
      args: [
        "skill",
        "add",
        ...(source === YOUR_SKILLS ? [] : [source]),
        ...selection,
        ...to,
      ],
      names,
      source,
    };
  });
};
