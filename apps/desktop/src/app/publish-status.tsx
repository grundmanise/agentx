// The publish status at the right of the title bar: whether your own skills have edits to
// publish, read from the inventory's library and the Sources slice. With no account remote set,
// it says so and a click goes to Settings, where one is set. With skills to publish, "{n} to
// publish", which a click publishes all at once (a bare `skill publish`). Otherwise "Published",
// which a click checks for what your other machines published (`skill check-updates`).
import { StatusButton } from "#/components/status-button.tsx";
import { navigateTo } from "#/lib/navigate.ts";
import { accountRemoteOf } from "#/screens/skills/banners.ts";
import {
  PUBLISH_ALL,
  publishAll,
  toPublish,
  usePublishProgress,
} from "#/screens/skills/publish.ts";
import { useSkillRows } from "#/screens/skills/rows.ts";
import {
  checkOtherMachines,
  useCheckingUpdates,
} from "#/screens/skills/updates.ts";
import { loadLabel } from "#/store/loaders.ts";
import { usePending } from "#/store/runner.ts";
import { useSettling } from "#/store/settle.ts";
import { useAppStore } from "#/store/store.ts";

/** The status, as the store says it: no account remote, how many skills to publish, or none. */
export type PublishState =
  | { kind: "no-account-remote" }
  | { kind: "to-publish"; count: number }
  | { kind: "published" };

/** The publish status from the store; undefined until the inventory and the sources are read. */
export const usePublishState = (): PublishState | undefined => {
  const rows = useSkillRows();
  const inventory = useAppStore((s) => s.inventory);
  const accountRemote = useAppStore((s) => accountRemoteOf(s.sources));
  // The sources load once the checks pass: until their first answer, the status would guess.
  const loadingSources = usePending(loadLabel(["source", "list"]));
  const noSources = useAppStore((s) => s.sources.length === 0);
  if (inventory === undefined || (loadingSources && noSources)) {
    return undefined;
  }
  if (accountRemote === undefined) {
    return { kind: "no-account-remote" };
  }
  const count = toPublish(rows).length;
  return count > 0 ? { count, kind: "to-publish" } : { kind: "published" };
};

/** What the label says while Publish all runs, from its progress. */
const publishingLabel = (progress?: { current: number; total?: number }) =>
  progress?.total === undefined
    ? "Publishing…"
    : `Publishing ${progress.current} of ${progress.total}`;

/** Publish all: a bare `skill publish`. */
const publish = () => {
  void publishAll();
};

export const PublishStatus = () => {
  const state = usePublishState();
  const publishing = useSettling(PUBLISH_ALL);
  const progress = usePublishProgress();
  const checking = useCheckingUpdates();
  if (state === undefined) {
    return null;
  }
  if (state.kind === "no-account-remote") {
    return (
      <StatusButton
        tone="none"
        label="No account remote"
        onClick={() => {
          navigateTo("/settings");
        }}
      />
    );
  }
  if (publishing) {
    return (
      <StatusButton
        tone="pending"
        label={publishingLabel(progress)}
        busy
        onClick={publish}
      />
    );
  }
  if (state.kind === "to-publish") {
    return (
      <StatusButton
        tone="pending"
        label={`${state.count} to publish`}
        onClick={publish}
      />
    );
  }
  return (
    <StatusButton
      tone="published"
      label="Published"
      turning={checking}
      busy={checking}
      onClick={() => {
        void checkOtherMachines();
      }}
    />
  );
};
