// AGENTS in the detail pane: one toggle chip per configuration that places skills in its own
// directory, then Universal for the clients that read the library. A chip runs one command and
// shows pending until the command ends and the next inventory arrives; the inventory then says
// what the chip shows. A locked or displaced chip runs nothing and toasts why.
import type { Configuration } from "#/cli/events.gen.ts";
import { AgentChip } from "#/components/agent-chip.tsx";
import { AgentLogo, LogoStack } from "#/components/agent-logo.tsx";
import { notify } from "#/components/toast.tsx";
import {
  chipLabel,
  chipStateOf,
  chipTitle,
  DISPLACED_REASON,
  lockReason,
  universalTitle,
} from "#/screens/skills/placements.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";

/**
 * Adds or removes the skill's placement in `c`, with an Undo that does the opposite. Off: `skill
 * place <name> --to <id>`; on: `skill remove <name> --from <id>`.
 */
const toggle = (row: SkillRow, c: Configuration, add: boolean) => {
  void runUntilSettled(
    add
      ? ["skill", "place", row.name, "--to", c.id]
      : ["skill", "remove", row.name, "--from", c.id],
    {
      action: {
        label: "Undo",
        onClick: () => {
          toggle(row, c, !add);
        },
      },
      label: chipLabel(row, c),
      success: add
        ? `Added ${row.name} to ${c.name}`
        : `Removed ${row.name} from ${c.name}`,
    }
  );
};

const DirectChip = ({ row, c }: { row: SkillRow; c: Configuration }) => {
  const state = chipStateOf(row, c);
  const locked = lockReason(row);
  const pending = useSettling(chipLabel(row, c));
  return (
    <AgentChip
      logo={<AgentLogo client={c} size={28} />}
      name={c.name}
      state={state}
      title={chipTitle(row, c, state)}
      locked={locked !== undefined}
      pending={pending}
      onClick={() => {
        if (locked !== undefined) {
          notify(locked);
        } else if (state === "displaced") {
          notify(DISPLACED_REASON);
        } else if (!pending) {
          toggle(row, c, state === "off");
        }
      }}
    />
  );
};

export const AgentsSection = ({
  row,
  configurations,
  onRemove,
}: {
  row: SkillRow;
  configurations: Configuration[];
  /** Opens the Remove dialog: the Universal chip, on, removes the skill from the machine. */
  onRemove: () => void;
}) => {
  const direct = configurations.filter((c) => !c.reads_library);
  const universal = configurations.filter((c) => c.reads_library);
  const locked = lockReason(row);
  return (
    <div className="gap-agent-chip-wrap flex flex-wrap">
      {direct.map((c) => (
        <DirectChip key={c.id} row={row} c={c} />
      ))}
      {universal.length > 0 && (
        <AgentChip
          logo={<LogoStack clients={universal} size={28} />}
          name="Universal"
          state={row.universal ? "on" : "off"}
          title={universalTitle(row)}
          locked={locked !== undefined || !row.universal}
          onClick={() => {
            if (locked !== undefined) {
              notify(locked);
            } else if (row.universal) {
              onRemove();
            }
          }}
        />
      )}
    </div>
  );
};
