// What the Sources screen shows, projected from the store and nothing else: the source pills
// from the Sources slice, the facts of the open source, the cards of a listing, of your skills on
// the hub or of a search answer, each card's state from the inventory's library, and
// the `skill add` command lines a selection produces. Pure functions, so that every part of the screen reads a skill the
// same way and the tests pin the command lines.
import type {
  Configuration,
  Install,
  InstallableSkillEvent,
  LibraryEntry,
  SearchResult,
  Source,
} from "#/cli/events.gen.ts";
import { formatAgo } from "#/lib/dates.ts";
import { hostAndPath, shortSource } from "#/screens/skills/projection.ts";
import type { Listing } from "#/store/store.ts";

/** The key of the Hub pill and the source of its cards, beside the source ids and URLs. */
export const HUB_SKILLS = "your-skills";

/** The label of the Hub pill and of its cards' group. */
export const HUB_LABEL = "Hub";

export interface SourcePill {
  /** The source id, or HUB_SKILLS. */
  key: string;
  /** The source's short form, or "Hub". */
  label: string;
  /** The canonical URL; absent for Hub. */
  url?: string;
  lastFetched?: string;
  /** How many skills it holds, as the inventory counts them. */
  skills?: number;
  /** The entry `source list` gave: the shared source, or the hub for Hub. */
  source: Source;
}

const compareLabels = (a: string, b: string) =>
  a.localeCompare(b, undefined, { sensitivity: "base" });

/** A pill of the sources row: a source's, or a link being added with its short form. */
export type RowPill =
  | { kind: "source"; pill: SourcePill }
  | { kind: "adding"; link: string; label: string };

/**
 * The sources row: the source pills, then each link being added in the order it was, the place
 * its source's pill will take, so nothing moves once it is added.
 */
export const rowPills = (
  pills: readonly SourcePill[],
  adding: readonly string[]
): RowPill[] => [
  ...pills.map((pill): RowPill => ({ kind: "source", pill })),
  ...adding.map((link): RowPill => ({
    kind: "adding",
    label: shortSource(link),
    link,
  })),
];

/** In the order the sources were added. */
const compareAdded = (a: SourcePill, b: SourcePill) =>
  (a.source.added ?? "").localeCompare(b.source.added ?? "");

/**
 * Hub when a hub is set, then one pill per shared source in the order they were added. A pill's
 * skill count is the one the inventory gives its source.
 */
export const sourcePills = (sources: Source[]): SourcePill[] => {
  const shared = sources
    .filter((s) => s.hub !== true)
    .map((s): SourcePill => {
      const pill: SourcePill = {
        key: s.id,
        label: shortSource(s.url),
        source: s,
        url: s.url,
      };
      if (s.last_fetched !== undefined) {
        pill.lastFetched = s.last_fetched;
      }
      if (s.skills !== undefined) {
        pill.skills = s.skills;
      }
      return pill;
    });
  shared.sort(compareAdded);
  const hub = sources.find((s) => s.hub === true);
  if (hub === undefined) {
    return shared;
  }
  const hubPill: SourcePill = {
    key: HUB_SKILLS,
    label: HUB_LABEL,
    source: hub,
  };
  if (hub.last_fetched !== undefined) {
    hubPill.lastFetched = hub.last_fetched;
  }
  if (hub.skills !== undefined) {
    hubPill.skills = hub.skills;
  }
  return [hubPill, ...shared];
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

/** The Fetched fact of a source: "17 minutes ago", or "Not yet" before its first fetch. */
export const fetchedFact = (
  lastFetched: string | undefined,
  now: number
): string =>
  (lastFetched === undefined ? undefined : formatAgo(lastFetched, now)) ??
  "Not yet";

/**
 * The page of a source's repository: `https://<host>/<path>` without `.git`, from an HTTPS or SSH
 * URL; undefined for anything else, such as a path on this machine. An HTTPS URL keeps its port;
 * an SSH port is not the web server's.
 */
export const webUrl = (url: string): string | undefined => {
  const parsed = hostAndPath(url);
  if (parsed === undefined || parsed.host === "") {
    return undefined;
  }
  const host = url.startsWith("https://") ? new URL(url).host : parsed.host;
  const path = parsed.path.replace(/^\/+/u, "").replace(/\.git$/u, "");
  return path === "" ? undefined : `https://${host}/${path}`;
};

/** Each `/`-separated part of a ref or a path, escaped for a URL. */
const urlPath = (path: string) =>
  path.split("/").map(encodeURIComponent).join("/");

/**
 * The web page of a skill's directory at `ref` in the source at `url`: GitHub's `/tree/` page, or
 * GitLab's `/-/tree/` page on a host named for GitLab; undefined for any other host, whose pages
 * agentx does not know.
 */
export const skillPage = (
  url: string,
  ref: string,
  subpath: string
): string | undefined => {
  const page = webUrl(url);
  if (page === undefined) {
    return undefined;
  }
  const { host } = new URL(page);
  let tree: string | undefined;
  if (host === "github.com") {
    tree = "tree";
  } else if (host.split(".").includes("gitlab")) {
    tree = "-/tree";
  }
  if (tree === undefined) {
    return undefined;
  }
  const at = `${page}/${tree}/${urlPath(ref)}`;
  return subpath === "" ? at : `${at}/${urlPath(subpath)}`;
};

/** The ref a source's pages are read at: the commit it was fetched at, else its pin, else HEAD. */
export const pageRef = (source: Source | undefined): string =>
  source?.commit ?? source?.pin ?? "HEAD";

/**
 * The letter on a source's tile: the first letter or digit of its owner, the first part of the
 * URL's path, in upper case.
 */
export const ownerInitial = (url: string): string => {
  const parsed = hostAndPath(url);
  const owner = parsed?.path.split("/").find((part) => part.length > 0) ?? url;
  return (/[\p{L}\p{N}]/u.exec(owner)?.[0] ?? "?").toUpperCase();
};

/** The label of the checkbox that selects every selectable card. */
export const selectAllLabel = (selected: number, total: number): string => {
  if (selected === 0) {
    return `Select all ${total}`;
  }
  return selected >= total
    ? `All ${total} selected`
    : `${selected} of ${total} selected`;
};

/** The skills this machine installed from the source at `url`, A to Z. */
export const installedFrom = (
  url: string,
  library: LibraryEntry[]
): string[] => {
  const names = library
    .filter((e) => e.kind === "managed" && e.source === url)
    .map((e) => e.name);
  names.sort(compareLabels);
  return names;
};

/** The toast after a source is removed, with the `skills` removed from it. */
export const removedText = (skills: number): string =>
  skills === 0
    ? "Source removed"
    : `Source and ${count(skills, "skill")} removed`;

/**
 * Why a card cannot be selected, from the inventory's library: this skill is Added or Forked, or
 * another skill holds its name.
 */
export type CardStatus = "added" | "forked" | "name-taken";

export const statusLabels: Record<CardStatus, string> = {
  added: "Added",
  forked: "Forked",
  "name-taken": "Name taken",
};

/**
 * Where `source read` finds a skill's SKILL.md: the source as the command takes it (its id for a
 * listing's card, its URL for a search answer's), the skill's subpath as the CLI gave it, "" for
 * a skill at the root, and the tree it was listed at, which a fetch of the source can change.
 */
export interface SkillMdRef {
  source: string;
  subpath: string;
  tree: string;
}

/** Why a card's name is taken, as the text before and after the library skill that holds it. */
export interface TakenReason {
  before: string;
  /** The name of the library skill that holds the name. */
  skill: string;
  after: string;
}

export interface SkillCard {
  /**
   * The card's own key: its source and name, and for a skill read from a source, its subpath, as
   * two directories of one source can hold skills of the same name.
   */
  key: string;
  /** Where the card installs from: the source's canonical URL, or HUB_SKILLS. */
  source: string;
  name: string;
  description: string;
  /** Why it cannot be selected; absent for a selectable card. */
  status?: CardStatus;
  /** Why a card that cannot be selected cannot be. */
  title?: string;
  /** For a name a library skill holds: `title` around that skill's name, which opens it in Skills. */
  taken?: TakenReason;
  /** Where its SKILL.md is read from; absent for your skills on the hub. */
  skillMd?: SkillMdRef;
  /** The skill's directory on its source's web page, when agentx knows the host's pages. */
  page?: string;
  /** Whether another skill of the card's source has its name, in any case. */
  twin?: boolean;
  /** The fork id of a hub skill. */
  forkId?: string;
}

export const cardKey = (source: string, name: string, subpath?: string) =>
  subpath === undefined
    ? `${source}\u0000${name}`
    : `${source}\u0000${name}\u0000${subpath}`;

/** Why a card is not selectable: its status, and for a name taken, the skill that holds it. */
interface CardState {
  status: CardStatus;
  /** The library entry that holds the card's name, for "name-taken". */
  holder?: LibraryEntry;
}

/** Why a card whose name `holder` holds cannot be added, and how to free the name. */
const takenWhy = ({
  kind,
  name,
  source,
  subpath,
}: LibraryEntry): TakenReason => {
  if (kind === "unmanaged") {
    return {
      after:
        " folder that agentx doesn't manage. Import or remove it to add this skill.",
      before: "Your library has a ",
      skill: name,
    };
  }
  if (subpath === undefined || source === undefined) {
    return {
      after: " skill. Remove or rename it to add this skill.",
      before: "Your library has ",
      skill: name,
    };
  }
  return {
    after: ` skill from ${shortSource(source)}. Remove or rename it to add this skill.`,
    before: "Your library has ",
    skill: name,
  };
};

const card = (
  source: string,
  name: string,
  description: string,
  state: CardState | undefined,
  skillMd?: SkillMdRef,
  page?: string
): SkillCard => {
  const c: SkillCard = {
    description,
    key: cardKey(source, name, skillMd?.subpath),
    name,
    source,
  };
  if (skillMd !== undefined) {
    c.skillMd = skillMd;
  }
  if (page !== undefined) {
    c.page = page;
  }
  if (state !== undefined) {
    c.status = state.status;
    if (state.holder !== undefined) {
      c.taken = takenWhy(state.holder);
      c.title = `${c.taken.before}${c.taken.skill}${c.taken.after}`;
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
): CardState | undefined => {
  if (
    library.some(
      (e) => e.kind === "managed" && e.source === url && e.subpath === subpath
    )
  ) {
    return { status: "added" };
  }
  if (
    library.some(
      (e) => e.upstream === url && (e.upstream_subpath ?? "") === subpath
    )
  ) {
    return { status: "forked" };
  }
  const named = library.find((e) => e.name === name);
  return named === undefined
    ? undefined
    : { holder: named, status: "name-taken" };
};

/** A card's source and name in any case: the cards of one source that share a name share it. */
const nameKey = (c: SkillCard) => `${c.source}\u0000${c.name.toLowerCase()}`;

/** The cards with `twin` on each skill whose source holds another skill of its name. */
const withTwins = (cards: SkillCard[]): SkillCard[] => {
  const named = new Map<string, number>();
  for (const c of cards) {
    if (c.skillMd !== undefined) {
      named.set(nameKey(c), (named.get(nameKey(c)) ?? 0) + 1);
    }
  }
  return cards.map((c) =>
    (named.get(nameKey(c)) ?? 0) > 1 ? { ...c, twin: true } : c
  );
};

/** Whether two cards are skills of one source with one name, of which the library holds one. */
export const sameName = (a: SkillCard, b: SkillCard) =>
  a.key !== b.key && nameKey(a) === nameKey(b);

/** The cards, keeping only the first of each name of a source: what Select all selects. */
export const oneOfEachName = (
  cards: SkillCard[],
  taken: Iterable<SkillCard> = []
): SkillCard[] => {
  const seen = new Set([...taken].map(nameKey));
  return cards.filter((c) => {
    if (seen.has(nameKey(c))) {
      return false;
    }
    seen.add(nameKey(c));
    return true;
  });
};

/**
 * How `skill add` names a card: by its name, or for a twin by its directory in the source, with a
 * slash so that the CLI reads it as one.
 */
const selector = (c: SkillCard): string => {
  const subpath = c.skillMd?.subpath ?? "";
  if (c.twin !== true || subpath === "") {
    return c.name;
  }
  return subpath.includes("/") ? subpath : `${subpath}/`;
};

/** The cards of a source's listing, as serve's `listing` or `source skills` gave it. */
export const listingCards = (
  listing: Listing["skills"],
  url: string,
  library: LibraryEntry[],
  /** The ref the skills' pages are at: `pageRef` of the source. */
  ref = "HEAD"
): SkillCard[] =>
  withTwins(
    listing.map((s) =>
      card(
        url,
        s.name,
        s.description,
        sourceSkillStatus(url, s.subpath, s.name, library),
        { source: s.source, subpath: s.subpath, tree: s.tree },
        skillPage(url, ref, s.subpath)
      )
    )
  );

/**
 * The state of one of your skills on the hub, by its name: a library entry of the
 * name is in the way, unless it is the same skill installed from its upstream without edits,
 * which the install replaces.
 */
const installableStatus = (
  skill: InstallableSkillEvent,
  library: LibraryEntry[]
): CardState | undefined => {
  const named = library.find((e) => e.name === skill.name);
  if (named === undefined) {
    return undefined;
  }
  const sameUpstream =
    named.kind === "managed" &&
    skill.upstream !== undefined &&
    named.source === skill.upstream &&
    named.subpath === (skill.upstream_subpath ?? "");
  return sameUpstream && named.state === "current"
    ? undefined
    : { holder: named, status: "name-taken" };
};

/** The cards of your skills on the hub, as `skill list --remote` gave them. */
export const installableCards = (
  installable: InstallableSkillEvent[],
  library: LibraryEntry[]
): SkillCard[] =>
  installable.map((s) => ({
    ...card(
      HUB_SKILLS,
      s.name,
      s.upstream === undefined
        ? "hub skill"
        : `from ${shortSource(s.upstream)}`,
      installableStatus(s, library)
    ),
    forkId: s.fork_id,
  }));

/** The cards of a search answer, whose `source` is each skill's source URL. */
export const resultCards = (
  results: SearchResult[],
  library: LibraryEntry[],
  /** The ref a source's pages are at, by its URL: `pageRef` of the source. */
  refOf: (url: string) => string = () => "HEAD"
): SkillCard[] =>
  withTwins(
    results.map((r) =>
      card(
        r.source,
        r.name,
        r.description,
        sourceSkillStatus(r.source, r.subpath, r.name, library),
        { source: r.source, subpath: r.subpath, tree: r.tree },
        skillPage(r.source, refOf(r.source), r.subpath)
      )
    )
  );

const byName = (a: SkillCard, b: SkillCard) =>
  compareLabels(a.name, b.name) || a.key.localeCompare(b.key);

/**
 * The cards of the Sources list A to Z by name, whatever their state, so that a card keeps its
 * place when its skill is added or removed.
 */
export const nameOrder = (cards: SkillCard[]): SkillCard[] => {
  const ordered = [...cards];
  ordered.sort(byName);
  return ordered;
};

/** The cards in their order: selectable first, then by name. */
export const orderCards = (cards: SkillCard[]): SkillCard[] => {
  const ordered = [...cards];
  ordered.sort(
    (a, b) =>
      Number(a.status !== undefined) - Number(b.status !== undefined) ||
      byName(a, b)
  );
  return ordered;
};

/**
 * The library name of the skill an Added card installed: the managed entry of the card's source
 * and subpath, which `skill remove` takes. Undefined for any other card.
 */
export const addedName = (
  added: SkillCard,
  library: LibraryEntry[]
): string | undefined =>
  added.status === "added" && added.skillMd !== undefined
    ? library.find(
        (e) =>
          e.kind === "managed" &&
          e.source === added.source &&
          e.subpath === added.skillMd?.subpath
      )?.name
    : undefined;

/**
 * When a card's skill last entered and last left this machine's library, from the inventory's
 * installs: a skill of a source by its source URL and subpath, a hub skill by its fork id.
 * Undefined when this machine recorded neither, as for a skill added before it kept the dates.
 */
export const installOf = (
  skill: SkillCard,
  installs: Install[]
): Install | undefined => {
  if (skill.forkId !== undefined) {
    return installs.find((i) => i.fork_id === skill.forkId);
  }
  const subpath = skill.skillMd?.subpath;
  return subpath === undefined
    ? undefined
    : installs.find((i) => i.source === skill.source && i.subpath === subpath);
};

export interface CardGroup {
  /** The source URL, or HUB_SKILLS. */
  source: string;
  /** The source's short form, or "Hub". */
  label: string;
  cards: SkillCard[];
}

const sourceLabel = (source: string) =>
  source === HUB_SKILLS ? HUB_LABEL : shortSource(source);

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

const sameSet = (a: string[], b: string[]) =>
  a.length === b.length && a.every((id) => b.includes(id));

/**
 * The `--to` flags of a `skill add` for the chosen `targets`, in their order: none when they are
 * the `enabled` configurations, where `skill add` installs by default.
 */
export const toFlags = (targets: string[], enabled: string[]): string[] =>
  sameSet(targets, enabled) ? [] : targets.flatMap((id) => ["--to", id]);

/** One `--name` flag per skill a `skill add` installs, in their order. */
export const nameFlags = (names: string[]): string[] =>
  names.flatMap((name) => ["--name", name]);

/**
 * The `skill add` of the skills `selected` under a link the user gave, which adds the link's
 * source if it is not added: the link, one `--name` per card in card order, and `--to` as
 * `planAdds` gives it.
 */
export const linkAdd = ({
  link,
  selected,
  targets,
  enabled,
}: {
  link: string;
  selected: SkillCard[];
  targets: string[];
  enabled: string[];
}): AddPlan => {
  const cards = orderCards(selected);
  const names = cards.map((c) => c.name);
  return {
    args: [
      "skill",
      "add",
      link,
      ...nameFlags(cards.map(selector)),
      ...toFlags(targets, enabled),
    ],
    names,
    source: link,
  };
};

export interface AddPlan {
  /** The command line, without --json. */
  args: string[];
  /** The source the command installs from: its URL, or HUB_SKILLS. */
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
  const to = toFlags(targets, enabled);
  return groupCards(selected).map(({ source, cards: chosen }) => {
    const cards = orderCards(chosen);
    const names = cards.map((c) => c.name);
    const selection =
      allOf !== undefined && allOf.source === source
        ? [
            "--all",
            ...allOf.cards.flatMap((c) =>
              cards.some((s) => s.key === c.key)
                ? []
                : ["--except", selector(c)]
            ),
          ]
        : nameFlags(cards.map(selector));
    return {
      args: [
        "skill",
        "add",
        ...(source === HUB_SKILLS ? [] : [source]),
        ...selection,
        ...to,
      ],
      names,
      source,
    };
  });
};
