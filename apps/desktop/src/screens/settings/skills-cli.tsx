// The Skills from the skills CLI section of Settings: what the adoption preview (`adopt`, which
// writes nothing) found that agentx can take over, and Adopt all, which runs `adopt --all` with
// its progress. Adopting installs each skill again from its source and replaces the copy the other
// tool left (ADR 0012). The section shows only while the preview has candidates; the records the
// CLI refuses are listed with its reason.
import { useState } from "react";

import type { ProgressEvent } from "#/cli/events.gen.ts";
import { Spinner } from "#/components/spinner.tsx";
import { Button } from "#/components/ui/button.tsx";
import { plural } from "#/lib/plural.ts";
import { SettingsSection } from "#/screens/settings/section.tsx";
import { runUntilSettled, useSettling } from "#/store/settle.ts";
import {
  adoptionCandidates,
  adoptionRefusals,
  useAppStore,
} from "#/store/store.ts";

const ADOPT_ALL_LABEL = "adopt all";

/** "Adopting 2 of 7" from the run's last progress event, or "Adopting…" before the first. */
const progressText = (progress: ProgressEvent | undefined) => {
  if (progress === undefined) {
    return "Adopting…";
  }
  return progress.total === undefined
    ? `Adopting ${progress.current}`
    : `Adopting ${progress.current} of ${progress.total}`;
};

/**
 * Skills from the skills CLI: the count, Adopt all, and the refused records with their reason;
 * nothing while the preview has no candidate.
 */
export const SkillsCliSection = () => {
  const adoption = useAppStore((s) => s.adoption);
  const candidates = adoptionCandidates(adoption);
  const refused = adoptionRefusals(adoption);
  const pending = useSettling(ADOPT_ALL_LABEL);
  const [progress, setProgress] = useState<ProgressEvent>();
  if (candidates.length === 0) {
    return null;
  }
  const adoptAll = async () => {
    setProgress(undefined);
    await runUntilSettled(["adopt", "--all"], {
      label: ADOPT_ALL_LABEL,
      onProgress: setProgress,
      success: (events) =>
        `Adopted ${plural(
          events.filter((e) => e.type === "adoption" && e.state === "adopted")
            .length,
          "skill"
        )}`,
    });
    setProgress(undefined);
  };
  return (
    <SettingsSection
      title="Skills from the skills CLI"
      description="Skills the Vercel skills CLI installed that agentx can take over. Adopting installs each one again from its source and replaces the copy the skills CLI left."
    >
      <div className="flex items-center justify-between gap-3">
        <p className="type-body text-text-primary m-0">
          {`${candidates.length} can be adopted`}
        </p>
        <Button
          variant="secondary"
          disabled={pending}
          aria-busy={pending ? true : undefined}
          onClick={() => {
            void adoptAll();
          }}
        >
          {pending && <Spinner />}
          {pending ? progressText(progress) : "Adopt all"}
        </Button>
      </div>
      {refused.length > 0 && (
        <div className="flex flex-col gap-2">
          <p className="type-secondary text-text-muted m-0">
            {`agentx can't adopt ${plural(refused.length, "skill")}:`}
          </p>
          <ul
            aria-label="Can't adopt"
            className="m-0 flex list-none flex-col gap-2 p-0"
          >
            {refused.map((record) => (
              <li
                key={`${record.tool} ${record.name}`}
                className="flex min-w-0 flex-col gap-0.5"
              >
                <span className="type-row-name text-text-primary truncate">
                  {record.name}
                </span>
                <span className="type-secondary text-text-muted">
                  {record.reason ?? "The skills CLI's record names no source"}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </SettingsSection>
  );
};
