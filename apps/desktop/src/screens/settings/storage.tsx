// Storage: where the library is and how much disk the home repo takes, as the `machine` event
// reports them. The app computes nothing about the machine.
import { SettingsSection } from "#/screens/settings/section.tsx";
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
  <div className="flex min-w-0 flex-col gap-1">
    <dt className="type-secondary text-text-muted">{name}</dt>
    <dd className="type-technical text-text-secondary m-0 break-all">
      {value}
    </dd>
  </div>
);

export const StorageSection = () => {
  const machine = useAppStore((s) => s.machine);
  if (machine === undefined) {
    return null;
  }
  let repo = "Not created yet";
  if (machine.home_repo !== undefined) {
    repo =
      machine.home_repo_bytes === undefined
        ? "Size unknown"
        : sizeText(machine.home_repo_bytes);
  }
  return (
    <SettingsSection title="Storage">
      <dl className="gap-settings-section m-0 flex flex-col">
        <Place name="Library" value={machine.library} />
        <Place name="Home repo" value={repo} />
      </dl>
    </SettingsSection>
  );
};
