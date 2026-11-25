// Snapshot: Save snapshot and Apply snapshot (`save-snapshot.tsx`, `apply-snapshot.tsx`).
import { ApplySnapshotRow } from "#/screens/settings/apply-snapshot.tsx";
import { SaveSnapshotRow } from "#/screens/settings/save-snapshot.tsx";
import { SettingsCard } from "#/screens/settings/section.tsx";

export const SnapshotCard = () => (
  <SettingsCard title="Snapshot">
    <SaveSnapshotRow />
    <ApplySnapshotRow />
  </SettingsCard>
);
