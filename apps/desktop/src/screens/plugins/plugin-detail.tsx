import { useState } from "react";
import type { ReactNode } from "react";

import type { Configuration } from "#/cli/events.gen.ts";
import { AgentLogo } from "#/components/agent-logo.tsx";
import { FolderIcon } from "#/components/icons/folder.tsx";
import { Pill } from "#/components/pill.tsx";
import { SelectableLink } from "#/components/selectable-link.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "#/components/ui/table.tsx";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "#/components/ui/tabs.tsx";
import { revealInFinder, showInLabel } from "#/lib/reveal.ts";
import { PluginTile } from "#/screens/plugins/plugin-tile.tsx";
import type { PluginItem, PluginRow } from "#/screens/plugins/projection.ts";
import { openServer } from "#/screens/servers/open.ts";
import { openSkill } from "#/screens/skills/open.ts";

/** One detail under the title: its label, with its value under it. */
const Detail = ({
  label,
  children,
}: {
  label: string;
  children: ReactNode;
}) => (
  <div className="flex min-w-0 flex-col gap-1">
    <dt className="type-secondary text-text-hint">{label}</dt>
    <dd className="type-technical-sm text-text-secondary m-0 flex min-h-5 min-w-0 flex-wrap items-center gap-2 break-all">
      {children}
    </dd>
  </div>
);

/** An agent configuration, always with its logo. */
const Agent = ({ configuration }: { configuration: Configuration }) => (
  <span className="inline-flex items-center gap-2">
    <AgentLogo client={configuration} size={16} decorative />
    {configuration.name}
  </span>
);

/**
 * A tab's table: a name column and a second one. A skill's or server's name is a link that opens
 * it in Skills or MCP servers; a hook opens nothing.
 */
const ItemTable = ({
  label,
  columns,
  items,
  onOpen,
}: {
  label: string;
  columns: [string, string];
  items: PluginItem[];
  onOpen?: (item: PluginItem) => void;
}) => (
  <Table aria-label={label} scroll>
    <TableHeader>
      <TableRow tone="header">
        <TableHead sticky>{columns[0]}</TableHead>
        <TableHead sticky>{columns[1]}</TableHead>
      </TableRow>
    </TableHeader>
    <TableBody>
      {items.map((item) => (
        <TableRow key={item.key}>
          <TableCell scope="row">
            {onOpen === undefined ? (
              <span className="type-technical text-text-primary">
                {item.name}
              </span>
            ) : (
              <Button
                variant="link"
                size="inline"
                onClick={() => {
                  onOpen(item);
                }}
              >
                <span className="type-row-name text-text-primary">
                  {item.name}
                </span>
              </Button>
            )}
          </TableCell>
          <TableCell>
            <span className="text-text-muted">{item.meta}</span>
          </TableCell>
        </TableRow>
      ))}
    </TableBody>
  </Table>
);

/**
 * The open plugin (DESIGN.md §6 "Plugins screen"): its tile, name, version and Disabled pills;
 * under them one wrapped row of details, the agent with its logo, the marketplace, the path, which
 * shows it in Finder, and the other agents that hold the same plugin; then a tab for each of its
 * skills, MCP servers and hooks that it has, each a table. Only the table scrolls: the title,
 * details and tabs stay put, and the table's column captions pin to its top.
 */
export const PluginDetail = ({
  row,
  configuration,
  twins,
  configurations,
  onSelect,
}: {
  row: PluginRow;
  /** The agent configuration the plugin is installed in. */
  configuration: Configuration;
  /** The same plugin in other agent configurations. */
  twins: PluginRow[];
  configurations: Configuration[];
  onSelect: (row: PluginRow) => void;
}) => {
  const tabs = (
    [
      { count: row.skills.length, label: "Skills", value: "skills" },
      { count: row.servers.length, label: "MCP servers", value: "servers" },
      { count: row.hooks.length, label: "Hooks", value: "hooks" },
    ] as const
  ).filter((t) => t.count > 0);
  const [tab, setTab] = useState<string>(tabs[0]?.value ?? "skills");
  // A refresh that empties the open tab moves to the first one left, and stays there.
  const first = tabs[0]?.value;
  if (first !== undefined && !tabs.some((t) => t.value === tab)) {
    setTab(first);
  }
  return (
    <article
      aria-label={row.name}
      className="pt-detail-top px-detail pb-detail-bottom max-w-detail gap-detail-section flex h-full min-h-0 flex-col"
    >
      <header className="gap-detail-section flex flex-none flex-col">
        <div className="flex flex-wrap items-center gap-3">
          <PluginTile name={row.name} icon={row.icon} size="detail" />
          <h1 className="type-plugin-name text-text-primary m-0 break-all select-text">
            {row.name}
          </h1>
          <Pill tone="version" face="mono" size="detail">
            {row.version === "" ? "no version" : row.version}
          </Pill>
          {row.disabled && (
            <Pill tone="disabled" size="detail">
              Disabled
            </Pill>
          )}
        </div>
        <dl
          aria-label="Details"
          className="gap-x-plugin-details gap-y-plugin-details-y m-0 flex flex-wrap"
        >
          <Detail label="Agent">
            <Agent configuration={configuration} />
          </Detail>
          <Detail label="Marketplace">
            {row.marketplace === "" ? "local" : row.marketplace}
          </Detail>
          <Detail label="Path">
            <span className="text-text-caption flex flex-none">
              <FolderIcon size="xs" />
            </span>
            <SelectableLink
              href={`file://${row.path}`}
              title={showInLabel()}
              tone="secondary"
              onOpen={() => {
                void revealInFinder(row.path);
              }}
            >
              {row.path}
            </SelectableLink>
          </Detail>
          {twins.length > 0 && (
            <Detail label="Also installed in">
              {twins.map((twin) => {
                const twinConfiguration = configurations.find(
                  (c) => c.id === twin.configuration
                );
                return (
                  <Button
                    key={twin.key}
                    variant="link"
                    size="inline"
                    onClick={() => {
                      onSelect(twin);
                    }}
                  >
                    {twinConfiguration === undefined ? (
                      twin.agent
                    ) : (
                      <Agent configuration={twinConfiguration} />
                    )}
                    {twin.version !== "" && ` ${twin.version}`}
                  </Button>
                );
              })}
            </Detail>
          )}
        </dl>
      </header>
      {tabs.length > 0 && (
        <Tabs
          fill
          value={tab}
          onValueChange={(value) => {
            setTab(value);
          }}
        >
          <TabsList aria-label="Contents">
            {tabs.map((t) => (
              <TabsTrigger key={t.value} value={t.value}>
                {t.label}
                <span className="type-technical-sm text-text-caption">
                  {t.count}
                </span>
              </TabsTrigger>
            ))}
          </TabsList>
          <TabsContent value="skills" fill>
            <ItemTable
              label="Skills"
              columns={["Skill", "Description"]}
              items={row.skills}
              onOpen={(item) => {
                openSkill(item.key);
              }}
            />
          </TabsContent>
          <TabsContent value="servers" fill>
            <ItemTable
              label="MCP servers"
              columns={["Server", "Transport"]}
              items={row.servers}
              onOpen={(item) => {
                openServer(item.key);
              }}
            />
          </TabsContent>
          <TabsContent value="hooks" fill>
            <ItemTable
              label="Hooks"
              columns={["Hook", "Event"]}
              items={row.hooks}
            />
          </TabsContent>
        </Tabs>
      )}
    </article>
  );
};
