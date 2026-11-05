import { useId } from "react";

import { Pill } from "#/components/pill.tsx";
import { TreeRow } from "#/components/tree-row.tsx";
import type { TreeTone } from "#/components/tree-row.tsx";
import { PluginTile } from "#/screens/plugins/plugin-tile.tsx";
import type { PluginCard, PluginItem } from "#/screens/plugins/projection.ts";
import { openServer } from "#/screens/servers/open.ts";
import { openSkill } from "#/screens/skills/open.ts";

/** The captions of the three groups, in the order they come. */
const captions: Record<TreeTone, string> = {
  hooks: "HOOKS",
  servers: "SERVERS",
  skills: "SKILLS",
};

/** A group of the contents: a caption and its tree rows; nothing when the plugin has none. */
const Group = ({
  tone,
  items,
  onOpen,
}: {
  tone: TreeTone;
  items: PluginItem[];
  /** Opens an item; a group without one (hooks) is display only. */
  onOpen?: (item: PluginItem) => void;
}) => {
  const id = useId();
  if (items.length === 0) {
    return null;
  }
  return (
    <section aria-labelledby={id} className="flex flex-col">
      <h3
        id={id}
        className="type-caption text-text-caption m-0 pt-3 pb-1.5 pl-2.5"
      >
        {captions[tone]}
      </h3>
      <ul className="m-0 flex list-none flex-col p-0">
        {items.map((item, i) => (
          <li key={item.key} className="flex">
            <TreeRow
              tone={tone}
              name={item.name}
              meta={item.meta}
              last={i === items.length - 1}
              onOpen={
                onOpen === undefined
                  ? undefined
                  : () => {
                      onOpen(item);
                    }
              }
            />
          </li>
        ))}
      </ul>
    </section>
  );
};

/**
 * One plugin in one agent configuration (DESIGN.md §6 "Plugin card"): its tile, the Disabled and
 * version pills, its name, the agent and marketplace it came from, then the skills, servers and
 * hooks it brings. Clicking a skill opens it in Skills, a server in MCP
 * servers; nothing here installs, removes, enables or disables anything.
 */
export const PluginCardView = ({ card }: { card: PluginCard }) => {
  const hasContents =
    card.skills.length + card.servers.length + card.hooks.length > 0;
  return (
    <article
      aria-label={card.name}
      className="bg-card-bg border-card-border rounded-plugin-card flex flex-col overflow-hidden border"
    >
      <div className="p-plugin-card flex flex-col gap-3.5">
        <div className="flex items-start justify-between gap-3">
          <PluginTile name={card.name} icon={card.icon} />
          <span className="flex flex-wrap items-center justify-end gap-1.5">
            {card.disabled && <Pill tone="disabled">Disabled</Pill>}
            <Pill tone="version" face="mono">
              {card.version === "" ? "no version" : card.version}
            </Pill>
          </span>
        </div>
        <div className="flex flex-col gap-1">
          <h2 className="type-plugin-name text-card-title m-0 truncate">
            {card.name}
          </h2>
          <span className="type-secondary text-text-muted truncate">
            {`${card.agent} · ${card.marketplace === "" ? "local" : card.marketplace}`}
          </span>
        </div>
      </div>
      {hasContents && (
        <div className="border-card-border flex flex-col border-t px-3 pt-1 pb-3">
          <Group
            tone="skills"
            items={card.skills}
            onOpen={(item) => {
              openSkill(item.key);
            }}
          />
          <Group
            tone="servers"
            items={card.servers}
            onOpen={(item) => {
              openServer(item.key);
            }}
          />
          <Group tone="hooks" items={card.hooks} />
        </div>
      )}
    </article>
  );
};
