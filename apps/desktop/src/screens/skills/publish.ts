// Publishing your own skills: what the Publish dialog of one skill (publish-dialog.tsx) and the
// publish of every skill at once share. Publish all is a bare `skill publish`, which the title
// bar's publish status and the palette run: it covers your own skills edited since they were
// published, and leaves out each one never published, which a warning names
// (docs/spec/cli-contract.md, "Publishing"). One toast says how it went, in the words the dialog
// uses for one skill; the list redraws from the inventory that follows.
import { create } from "zustand";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { LogEvent, PublishEvent } from "#/cli/events.gen.ts";
import { notify } from "#/components/toast.tsx";
import type { ErrorToast, ToastAction } from "#/components/toast.tsx";
import { againRule, doctorRule, rewriteHint } from "#/lib/app-hint.ts";
import type { HintRule } from "#/lib/app-hint.ts";
import { navigateTo } from "#/lib/navigate.ts";
import {
  accountRemoteOf,
  hasEditsToPublish,
} from "#/screens/skills/banners.ts";
import { openSkill } from "#/screens/skills/open.ts";
import { shortSource } from "#/screens/skills/projection.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { currentRows } from "#/screens/skills/rows.ts";
import { addSource, updateSkill } from "#/screens/skills/updates.ts";
import type { Outcome } from "#/store/runner.ts";
import { runUntilSettled } from "#/store/settle.ts";
import { addActivity, useAppStore } from "#/store/store.ts";

/** The label of Publish all, which shows as pending while the bare publish runs. */
export const PUBLISH_ALL = "publish all";

/**
 * The hints of `skill publish` that name a command, matched whole, and the app's words for each:
 * those of one skill, which the Publish dialog shows, and those of a bare publish, which Publish
 * all shows. A hint no rule matches shows as the CLI wrote it.
 */
const publishHintRules: HintRule[] = [
  {
    app: () => "add the source again, then publish again",
    cli: /^run 'agentx source add .+' to add it again, then publish again$/u,
  },
  {
    app: () => "publish again to publish it as it is now",
    cli: /^run 'agentx skill publish .+' again to publish it as it is now$/u,
  },
  {
    app: () =>
      "use Check updates to record that, or Remove to remove the skill",
    cli: /^run 'agentx skill check-updates' to record that, or 'agentx skill remove .+' to remove it$/u,
  },
  {
    app: ({ why }) => `update the skill to ${why ?? ""}, then publish again`,
    cli: /^run 'agentx skill update .+' to (?<why>take the change in|get them), then publish again$/u,
  },
  {
    app: () =>
      "add the source again with the branch to publish to after a #, then update the skill from it and publish again",
    cli: /^add the source at the branch to publish to with 'agentx source add .+#<branch>', then update the skill from it and publish again$/u,
  },
  {
    app: () =>
      "see the state of each skill in Skills, then publish the rest one at a time",
    cli: /^run 'agentx skill list' to see the state of each skill, then publish the rest one at a time$/u,
  },
  {
    app: ({ first }) =>
      `${first ?? ""}; the edits were recorded, so publish again once the account remote can be reached`,
    cli: /^(?<first>.+); the edits were recorded, so run 'agentx skill publish(?: .+)?' again once the account remote can be reached$/u,
  },
  {
    app: ({ what }) => `publish again once ${what ?? ""} takes it`,
    cli: /^once (?<what>.+) takes it, run 'agentx skill publish(?: .+)?' again$/u,
  },
  {
    app: ({ first }) => `${first ?? ""}, then publish again`,
    cli: /^(?<first>.+), then run 'agentx skill publish(?: .+)?' again$/u,
  },
  {
    app: ({ rest }) =>
      rest === undefined ? "publish again" : `publish again; ${rest}`,
    cli: /^run 'agentx skill publish(?: .+?)?' again(?:; (?<rest>.+))?$/u,
  },
  doctorRule,
  againRule("publish again"),
];

/**
 * The error toast of a failed publish whose hint names a command: the CLI's message and its
 * warnings, the hint in the app's words, and Add source for a skill whose source was removed
 * (exit 5), `source` being that skill's. Undefined leaves the runner's own toast.
 */
export const publishFailure = (
  outcome: Outcome,
  source?: string
): ErrorToast | undefined => {
  const error = outcome.events.find((e) => e.type === "error");
  const hint = rewriteHint(error?.hint, publishHintRules);
  const action =
    outcome.exitCode === EXIT_CODES.not_found && source !== undefined
      ? { action: addSource(source) }
      : {};
  return error === undefined || hint === error.hint
    ? undefined
    : {
        ...action,
        hint,
        message: error.message,
        warnings: outcome.logs
          .filter((l) => l.level === "warn")
          .map((l) => l.message),
      };
};

/**
 * The toast of a skill whose newer version is where it publishes to: it needs an update before a
 * publish. For one of your own skills that is the account remote; for a skill of a source, the
 * source, which `source` names by its URL.
 */
export const changedElsewhere = (name: string, source?: string) =>
  `There is a newer version of ${name} skill ${source === undefined ? "on the account remote" : `in ${shortSource(source)}`}. Update it first, then publish your changes.`;

/** The toast of a publish that sent nothing: the skill already matches what is published. */
export const alreadyPublished = (name: string) =>
  `${name} skill was not published. It already matches the published version.`;

/** A Publish all that pushed no skill: none was published, and why, when it judged some. */
const nonePublished = (judged: number) =>
  judged === 0
    ? "No skills were published"
    : "No skills were published. They already match the published versions.";

/**
 * Whether a publish was refused for want of an account remote: refused before any skill was
 * judged, with none set or with the CLI saying so.
 */
export const refusedForAccountRemote = (
  outcome: Outcome,
  accountRemoteSet: boolean
) => {
  const judged = outcome.events.some((e) => e.type === "publish");
  const error = outcome.events.find((e) => e.type === "error");
  return (
    outcome.exitCode === EXIT_CODES.refused &&
    !judged &&
    (!accountRemoteSet || error?.message === "no account remote is set")
  );
};

/** "Set an account remote to publish", with Open Settings. */
export const notifyNoAccountRemote = () => {
  notify("Set an account remote to publish", {
    label: "Open Settings",
    onClick: () => {
      navigateTo("/settings");
    },
  });
};

/** Your own skills a bare publish covers: those edited since they were published. */
export const toPublish = (rows: SkillRow[]) => rows.filter(hasEditsToPublish);

const plural = (n: number, one: string, many: string) =>
  n === 1 ? `${n} ${one}` : `${n} ${many}`;

const NOT_PUBLISHED = /^(?<name>\S+) is not published:/u;

/** The skills a bare publish left out as never published, by the warnings that name them. */
const leftOut = (logs: LogEvent[]) =>
  logs
    .filter((log) => log.level === "warn")
    .map((log) => NOT_PUBLISHED.exec(log.message)?.groups?.name)
    .filter((name) => name !== undefined);

/** The last `progress` event of phase `publish` of the running Publish all: how far it got. */
interface Progress {
  current: number;
  total?: number;
}

const useProgressStore = create<{ progress?: Progress }>()(() => ({}));

/** How far the running Publish all got; undefined before its first skill and once it ends. */
export const usePublishProgress = () => useProgressStore((s) => s.progress);

/** Update, for a skill another machine published to first: `skill update <name>`. */
const updateAction = (name: string): ToastAction | undefined => {
  const row = currentRows().find((r) => r.key === name);
  return row === undefined
    ? undefined
    : {
        label: "Update",
        onClick: () => {
          void updateSkill(row);
        },
      };
};

/** Show, which opens the first skill the publish left out. */
const showAction = (name: string): ToastAction => ({
  label: "Show",
  onClick: () => {
    openSkill(name);
  },
});

/**
 * The toast of a Publish all that ended. A skill the account remote declined, or one this machine
 * may not push, keeps the runner's error toast, in the CLI's words; so does any other failure.
 */
const reportPublishAll = (outcome: Outcome) => {
  const results = outcome.events.filter(
    (e): e is PublishEvent => e.type === "publish"
  );
  const failed = results.some(
    (e) =>
      e.outcome === "declined" ||
      e.outcome === "denied" ||
      e.outcome === "refused"
  );
  const moved = results.find(
    (e) => e.outcome === "moved" || e.outcome === "behind"
  );
  const skipped = leftOut(outcome.logs);
  const suffix =
    skipped.length === 0 ? "" : ` · ${skipped.length} not published yet`;
  const ok = outcome.exitCode === EXIT_CODES.ok;
  if (failed || (!ok && moved === undefined)) {
    return;
  }
  if (moved !== undefined) {
    const text = `${changedElsewhere(moved.name)}${suffix}`;
    notify(text, updateAction(moved.name));
    // A refused publish is in the activity list already, in the CLI's words.
    if (ok) {
      addActivity("attention", text);
    }
    return;
  }
  const pushed = results.filter((e) => e.outcome === "pushed").length;
  const text = `${pushed === 0 ? nonePublished(results.length) : `Published ${plural(pushed, "skill", "skills")}`}${suffix}`;
  const [first] = skipped;
  notify(text, first === undefined ? undefined : showAction(first));
  addActivity(pushed === 0 ? "attention" : "success", text);
};

/**
 * Publish all: a bare `skill publish`, one run at a time. Its progress shows in the title bar
 * while it runs, then one toast says how it went.
 */
export const publishAll = async () => {
  if (Object.hasOwn(useAppStore.getState().pending, PUBLISH_ALL)) {
    return;
  }
  useProgressStore.setState({ progress: undefined });
  const outcome = await runUntilSettled(["skill", "publish"], {
    failure: (ended) => publishFailure(ended),
    label: PUBLISH_ALL,
    onProgress: (event) => {
      if (event.phase === "publish") {
        useProgressStore.setState({
          progress: { current: event.current, total: event.total },
        });
      }
    },
  });
  useProgressStore.setState({ progress: undefined });
  const accountRemoteSet =
    accountRemoteOf(useAppStore.getState().sources) !== undefined;
  if (refusedForAccountRemote(outcome, accountRemoteSet)) {
    notifyNoAccountRemote();
    return;
  }
  reportPublishAll(outcome);
};
