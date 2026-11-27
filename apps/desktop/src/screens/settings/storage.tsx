// Storage, in the This machine card: where the library is, as the inventory's machine says, and
// how much disk the store takes, as the `machine` event reports it. The store is measured each
// time Settings shows, and the row says so until the measurement ends. The app computes nothing
// about the machine.
import { useEffect } from "react";

import { SettingsRow } from "#/screens/settings/section.tsx";
import { measureStore } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";
import type { AppState } from "#/store/store.ts";

const UNITS = ["KiB", "MiB", "GiB", "TiB"] as const;

/** A size as the CLI prints it: bytes below one KiB, then one decimal in KiB, MiB, GiB or TiB. */
export const sizeText = (bytes: number): string => {
  if (bytes < 1024) {
    return `${bytes} B`;
  }
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  return `${value.toFixed(1)} ${UNITS[unit]}`;
};

const Place = ({ name, value }: { name: string; value: string }) => (
  <SettingsRow
    name={name}
    control={
      <span className="type-technical text-text-secondary text-right break-all select-text">
        {value}
      </span>
    }
  />
);

/** What the Store row says: that the store is measured, then its size or why there is none. */
const storeText = (store: AppState["store"]): string => {
  if (store === undefined) {
    return "Measuring…";
  }
  if (store === "unknown") {
    return "Size unknown";
  }
  if (store.store === undefined) {
    return "Not created yet";
  }
  return store.store_bytes === undefined
    ? "Size unknown"
    : sizeText(store.store_bytes);
};

export const StorageRows = () => {
  const library = useAppStore((s) => s.inventory?.machine.library);
  const store = useAppStore((s) => s.store);
  // Measured each time Settings shows: an effect runs again when a kept screen shows again. What
  // was measured is forgotten as Settings hides, so the next time the row measures from the start.
  useEffect(() => measureStore(), []);
  if (library === undefined) {
    return null;
  }
  return (
    <>
      <Place name="Library" value={library} />
      <Place name="Store" value={storeText(store)} />
    </>
  );
};
