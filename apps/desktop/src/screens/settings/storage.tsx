// Storage, in the This machine card: where the library is, as the inventory's machine says, and how much disk the store
// takes, as the `machine` event reports it each time Settings shows. The app computes nothing
// about the machine.
import { useEffect } from "react";

import { SettingsRow } from "#/screens/settings/section.tsx";
import { loadStore } from "#/store/loaders.ts";
import { useAppStore } from "#/store/store.ts";

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

export const StorageRows = () => {
  const library = useAppStore((s) => s.inventory?.machine.library);
  const store = useAppStore((s) => s.store);
  // Read each time Settings shows: an effect runs again when a kept screen shows again.
  useEffect(() => {
    void loadStore();
  }, []);
  if (library === undefined) {
    return null;
  }
  let repo = "Not created yet";
  if (store === undefined) {
    repo = "Size unknown";
  } else if (store.store !== undefined) {
    repo =
      store.store_bytes === undefined
        ? "Size unknown"
        : sizeText(store.store_bytes);
  }
  return (
    <>
      <Place name="Library" value={library} />
      <Place name="Store" value={repo} />
    </>
  );
};
