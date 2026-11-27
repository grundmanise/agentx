// Storage, in the This machine card: where the library and the store are, as the inventory's
// machine says, and how much disk the store takes, as the `machine` event reports it. The size is
// measured each time Settings shows, and the row says so until the measurement ends. The app
// computes nothing about the machine.
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

/** The line under Store: its size, or that it is measured. */
const sizeLine = (bytes: AppState["storeBytes"]) => {
  if (bytes === undefined) {
    return "Measuring…";
  }
  return bytes === "unknown" ? "Size unknown" : sizeText(bytes);
};

const Place = ({
  name,
  line,
  value,
}: {
  name: string;
  line?: string;
  value: string;
}) => (
  <SettingsRow
    name={name}
    line={line}
    control={
      <span className="type-technical text-text-secondary text-right break-all select-text">
        {value}
      </span>
    }
  />
);

export const StorageRows = () => {
  const library = useAppStore((s) => s.inventory?.machine.library);
  const store = useAppStore((s) => s.inventory?.machine.store);
  const bytes = useAppStore((s) => s.storeBytes);
  const created = store !== undefined;
  // Measured each time Settings shows: an effect runs again when a kept screen shows again. The
  // size is forgotten as Settings hides, so the next time it shows the row measures from the start.
  useEffect(() => (created ? measureStore() : undefined), [created]);
  if (library === undefined) {
    return null;
  }
  return (
    <>
      <Place name="Library" value={library} />
      {store === undefined ? (
        <Place name="Store" value="Not created yet" />
      ) : (
        <Place name="Store" line={sizeLine(bytes)} value={store} />
      )}
    </>
  );
};
