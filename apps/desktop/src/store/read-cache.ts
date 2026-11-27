// The kept reads: what the read of a skill, a source's listing and a SKILL.md read returned, kept
// in IndexedDB for the next launch. A read for the same version as one kept is served from here,
// so a skill selected after a relaunch shows at once. One entry is kept per
// skill or source and kind of read, the latest; past CACHE_BYTES the least recently used go first.
// What another build of the app kept is dropped, since its events may have another shape. It is
// a cache: every failure here is a miss, and the app works the same without it.
import { createStorage } from "unstorage";
import indexedDbDriver from "unstorage/drivers/indexedb";

/** The kinds of read kept, each under its own keys. */
export type ReadKind = "installable" | "listing" | "skill" | "skill-md";

/** How much the kept reads may hold, in UTF-16 code units of their JSON: about 25 MB. */
export const CACHE_BYTES = 25 * 1024 * 1024;
/** How long the index waits for more changes before it is written. */
const INDEX_WRITE_MS = 1000;
const INDEX_KEY = "index";

interface Entry<T = unknown> {
  build: string;
  version: string;
  value: T;
}

/** When a kept read was last used, and its size: what eviction goes by. */
interface Use {
  at: number;
  size: number;
}

/** The index as it is written: the build that wrote it, and the use of each read it names. */
interface StoredIndex {
  build: string;
  entries: Record<string, Use>;
}

/** The index as this run holds it. */
interface Index {
  build: string;
  entries: Map<string, Use>;
}

const storage = createStorage({
  driver: indexedDbDriver({ dbName: "agentx", storeName: "reads" }),
});

const keyOf = (kind: ReadKind, id: string) =>
  `${kind}:${encodeURIComponent(id)}`;

/**
 * Reads the index. Another build's reads are all dropped, and so is a read the index does not
 * name, such as one written as the app quit.
 */
const readIndex = async (): Promise<Index | undefined> => {
  const build = import.meta.env.APP_BUILD;
  try {
    const stored = await storage.getItem<StoredIndex>(INDEX_KEY);
    if (stored?.build !== build) {
      await storage.clear();
      return { build, entries: new Map() };
    }
    const entries = new Map(Object.entries(stored.entries));
    const keys = await storage.getKeys();
    await Promise.all(
      keys
        .filter((key) => key !== INDEX_KEY && !entries.has(key))
        .map(async (key) => {
          await storage.removeItem(key);
        })
    );
    return { build, entries };
  } catch {
    return undefined;
  }
};

let index: Promise<Index | undefined> | undefined;
/** The index, read once a run; undefined when there is no storage. */
const theIndex = async () => {
  index ??= readIndex();
  return await index;
};

const writeIndex = async (kept: Index) => {
  try {
    await storage.setItem(INDEX_KEY, {
      build: kept.build,
      entries: Object.fromEntries(kept.entries),
    } satisfies StoredIndex);
  } catch {
    // No storage: the next launch drops the reads the index does not name.
  }
};

let indexTimer: ReturnType<typeof setTimeout> | undefined;
const scheduleIndexWrite = (kept: Index) => {
  clearTimeout(indexTimer);
  indexTimer = setTimeout(() => {
    void writeIndex(kept);
  }, INDEX_WRITE_MS);
};

/** The read of `kind` kept for `id` at `version`; undefined when none is kept. */
export const cachedRead = async <T>(
  kind: ReadKind,
  id: string,
  version: string
): Promise<T | undefined> => {
  const kept = await theIndex();
  const key = keyOf(kind, id);
  const use = kept?.entries.get(key);
  if (kept === undefined || use === undefined) {
    return undefined;
  }
  try {
    const entry = await storage.getItem<Entry<T>>(key);
    if (entry?.build !== kept.build || entry.version !== version) {
      return undefined;
    }
    use.at = Date.now();
    scheduleIndexWrite(kept);
    return entry.value;
  } catch {
    return undefined;
  }
};

/**
 * Keeps the read of `kind` for `id` at `version`, in place of the one kept before, and drops the
 * least recently used reads while the kept ones are over CACHE_BYTES. A read larger than a
 * quarter of that is not kept.
 */
export const keepRead = async (
  kind: ReadKind,
  id: string,
  version: string,
  value: unknown
) => {
  const kept = await theIndex();
  if (kept === undefined) {
    return;
  }
  const key = keyOf(kind, id);
  const size = JSON.stringify(value).length;
  if (size > CACHE_BYTES / 4) {
    // The read kept before is older than this one, which is not kept: neither shows next launch.
    if (kept.entries.delete(key)) {
      await writeIndex(kept);
      try {
        await storage.removeItem(key);
      } catch {
        // No storage: the next launch drops the value, which the index no longer names.
      }
    }
    return;
  }
  const mine = { at: Date.now(), size };
  kept.entries.set(key, mine);
  const byUse = [...kept.entries];
  byUse.sort(([, a], [, b]) => a.at - b.at);
  let total = byUse.reduce((sum, [, use]) => sum + use.size, 0);
  const dropped: string[] = [];
  for (const [other, use] of byUse) {
    if (total <= CACHE_BYTES) {
      break;
    }
    if (other !== key) {
      total -= use.size;
      dropped.push(other);
      kept.entries.delete(other);
    }
  }
  try {
    const entry: Entry = { build: kept.build, value, version };
    await Promise.all([
      storage.setItem(key, entry),
      ...dropped.map(async (other) => {
        await storage.removeItem(other);
      }),
    ]);
  } catch {
    // No storage, or no room in it: the read is not kept, and the index does not name it, unless
    // a later read of the same key took its place meanwhile.
    if (kept.entries.get(key) === mine) {
      kept.entries.delete(key);
    }
  }
  // The index names the read as soon as it is written, so a read kept just before the app quits
  // is there next launch.
  await writeIndex(kept);
};
