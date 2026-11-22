// What the Sources screen shows, projected from the store and nothing else: the source pills
// from the Sources slice, the facts of the open source, the cards of a listing, of your skills on
// the account remote or of a search answer, each card's state from the inventory's library, and
// the `skill add` command lines a selection produces. Pure functions, so that every part of the screen reads a skill the
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
import { hostAndPath, shortSource } from "#/screens/skills/projection.ts";

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
  /** How many skills it holds: its listing's length, or your skills on the account remote. */
  skills?: number;
  /** The entry `source list` gave: the shared source, or the account remote for Personal. */
  source: SourceEvent;
}

const compareLabels = (a: string, b: string) =>
  a.localeCompare(b, undefined, { sensitivity: "base" });

/**
 * One pill per shared source, A to Z by short form, then Personal when an account remote is
 * set. A shared source's skill count is the length of its listing, once read: `source list`
 * reports none for a shared source.
 */
export const sourcePills = (
  sources: SourceEvent[],
  listings: Readonly<Record<string, readonly unknown[] | undefined>> = {}
): SourcePill[] => {
  const shared = sources
    .filter((s) => s.account !== true)
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
      const listing = listings[s.id];
      if (listing !== undefined) {
        pill.skills = listing.length;
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
  const yours: SourcePill = {
    key: YOUR_SKILLS,
    label: YOUR_SKILLS_LABEL,
    source: account,
  };
  if (account.last_fetched !== undefined) {
    yours.lastFetched = account.last_fetched;
  }
  if (account.skills !== undefined) {
    yours.skills = account.skills;
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
export const pageRef = (source: SourceEvent | undefined): string =>
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

/** Why a card cannot be selected, from the inventory's library. */
export type CardStatus = "added" | "forked" | "on-disk" | "name-in-use";

export const statusLabels: Record<CardStatus, string> = {
  added: "Added",
  forked: "Forked",
  "name-in-use": "Name in use",
  "on-disk": "Already in library",
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

export interface SkillCard {
  /**
   * The card's own key: its source and name, and for a skill read from a source, its subpath, as
   * two directories of one source can hold skills of the same name.
   */
  key: string;
  /** Where the card installs from: the source's canonical URL, or YOUR_SKILLS. */
  source: string;
  name: string;
  description: string;
  /** Why it cannot be selected; absent for a selectable card. */
  status?: CardStatus;
  /** Why a card that cannot be selected cannot be. */
  title?: string;
  /** Where its SKILL.md is read from; absent for your skills on the account remote. */
  skillMd?: SkillMdRef;
  /** The skill's directory on its source's web page, when agentx knows the host's pages. */
  page?: string;
  /**
   * The subpath of the skill at an earlier subpath of the same source that has this card's name,
   * which `skill add` installs instead: the card is in use by name, not added.
   */
  sharedWith?: string;
}

export const cardKey = (source: string, name: string, subpath?: string) =>
  subpath === undefined
    ? `${source}\u0000${name}`
    : `${source}\u0000${name}\u0000${subpath}`;

const card = (
  source: string,
  name: string,
  description: string,
  status: CardStatus | undefined,
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
  if (status !== undefined) {
    c.status = status;
    if (status === "name-in-use") {
      c.title = `Remove or rename ${name} first`;
    }
    if (status === "on-disk") {
      c.title = `A folder named ${name} is already in your library, added outside agentx. Import it in Skills to manage it, or remove it to add this one.`;
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

/** Where a skill sits in its source, for a reason that names it. */
const placeOf = (subpath: string) => (subpath === "" ? "the root" : subpath);

/**
 * The cards with each skill a source holds under the name of a skill at an earlier subpath, in
 * any case, as not selectable: the library holds one skill per name, and `skill add` installs
 * the first of a name and skips the rest. A card that cannot be selected already keeps its reason.
 */
const withSharedNames = (cards: SkillCard[]): SkillCard[] => {
  const nameOf = (c: SkillCard) => `${c.source}\u0000${c.name.toLowerCase()}`;
  const first = new Map<string, string>();
  for (const c of cards) {
    const subpath = c.skillMd?.subpath;
    const earlier = first.get(nameOf(c));
    if (subpath !== undefined && (earlier === undefined || subpath < earlier)) {
      first.set(nameOf(c), subpath);
    }
  }
  return cards.map((c) => {
    const earlier = first.get(nameOf(c));
    if (
      c.status !== undefined ||
      earlier === undefined ||
      earlier === c.skillMd?.subpath
    ) {
      return c;
    }
    return {
      ...c,
      sharedWith: earlier,
      status: "name-in-use",
      title: `The skill at ${placeOf(earlier)} in this source is named ${c.name} too, and only the first of a name can be added`,
    };
  });
};

/** The cards of a source's listing, as `source skills <id>` gave it. */
export const listingCards = (
  listing: SourceSkillEvent[],
  url: string,
  library: LibraryEntry[],
  /** The ref the skills' pages are at: `pageRef` of the source. */
  ref = "HEAD"
): SkillCard[] =>
  withSharedNames(
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
  library: LibraryEntry[],
  /** The ref a source's pages are at, by its URL: `pageRef` of the source. */
  refOf: (url: string) => string = () => "HEAD"
): SkillCard[] =>
  withSharedNames(
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
  const names = orderCards(selected).map((c) => c.name);
  return {
    args: [
      "skill",
      "add",
      link,
      ...nameFlags(names),
      ...toFlags(targets, enabled),
    ],
    names,
    source: link,
  };
};

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
  const to = toFlags(targets, enabled);
  return groupCards(selected).map(({ source, cards: chosen }) => {
    const cards = orderCards(chosen);
    const names = cards.map((c) => c.name);
    const selection =
      allOf !== undefined && allOf.source === source
        ? [
            "--all",
            // --except leaves out every skill of a name, so a name with a selected card stays in:
            // the CLI installs the first of it, which is the one that can be selected.
            ...allOf.cards.flatMap((c) =>
              cards.some(
                (s) =>
                  s.key === c.key ||
                  s.name.toLowerCase() === c.name.toLowerCase()
              )
                ? []
                : ["--except", c.name]
            ),
          ]
        : nameFlags(names);
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
