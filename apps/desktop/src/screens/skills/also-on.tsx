// Also on, the last row of a skill's DETAILS: the other machines that hold the skill, by the rule
// of src/screens/machines/across.ts, each a link that opens it on the Machines screen, with a
// pill after a machine that holds another version. No row when no other machine holds the skill,
// as when no hub is set.
import { useMemo } from "react";

import { Badge } from "#/components/ui/badge.tsx";
import { Button } from "#/components/ui/button.tsx";
import { alsoOn } from "#/screens/machines/across.ts";
import { openMachine } from "#/screens/machines/open.ts";
import { holdingOf, nameOf } from "#/screens/machines/projection.ts";
import type { Holding } from "#/screens/machines/projection.ts";
import { useMachines } from "#/screens/machines/use-machines.ts";
import { DetailRow } from "#/screens/skills/detail-row.tsx";
import type { SkillRow } from "#/screens/skills/projection.ts";

/** What this machine holds of `row`: its library entry, or for any other skill its name and content. */
const heldAs = (row: SkillRow): Holding =>
  row.entry === undefined
    ? {
        contentHash: row.node?.content_hash,
        logicalId: undefined,
        name: row.name,
        upstreamCommit: undefined,
      }
    : holdingOf(row.entry);

export const AlsoOn = ({ row }: { row: SkillRow }) => {
  const machines = useMachines();
  const elsewhere = useMemo(
    () => alsoOn(heldAs(row), machines),
    [row, machines]
  );
  if (elsewhere.length === 0) {
    return null;
  }
  return (
    <DetailRow label="Also on">
      <span className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
        {elsewhere.map(({ machine, version }) => (
          <span key={machine.id} className="flex min-w-0 items-center gap-2">
            <Button
              variant="link"
              size="inline"
              onClick={() => {
                openMachine(machine.id);
              }}
            >
              {nameOf(machine)}
            </Button>
            {version === "other" && (
              <Badge tone="neutral">Another version</Badge>
            )}
          </span>
        ))}
      </span>
    </DetailRow>
  );
};
