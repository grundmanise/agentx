// Puts on window the keys of the reads the kept reads' index names, read from IndexedDB with
// idb-keyval, as unstorage's driver writes them: a test waits for a read to be kept before a
// relaunch.
import { createStore, get } from "idb-keyval";

declare global {
  interface Window {
    keptReads: () => Promise<string[]>;
  }
}

const reads = createStore("agentx", "reads");

window.keptReads = async () => {
  const stored = await get<unknown>("index", reads);
  if (typeof stored !== "string") {
    return [];
  }
  const index: unknown = JSON.parse(stored);
  return typeof index === "object" &&
    index !== null &&
    "entries" in index &&
    typeof index.entries === "object" &&
    index.entries !== null
    ? Object.keys(index.entries)
    : [];
};
