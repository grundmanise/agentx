// What the MCP servers screen shows, projected from the store and nothing else: one row per
// logical server, however many agent configurations declare it and in however many versions. Pure
// functions over the applied inventory's `mcp_servers` nodes and the handshake failures of the
// last Check tools, so that every later change reads a server the same way. Nothing here
// predicts: a row changes only when the inventory does. No environment or header value is read:
// the CLI records only their names, and the screen shows neither.
import type {
  HandshakeReason,
  InventoryEvent,
  McpServer,
  ServerOccurrence,
  Transport,
} from "#/cli/events.gen.ts";
import type { HandshakeFailure } from "#/store/store.ts";

/** An agent configuration that declares a server, and whether it turned the declaration off. */
export interface ServerAgent {
  configuration: string;
  /** Every declaration of the server in this configuration has `enabled` false. */
  off: boolean;
}

/** Why a declaration's handshake failed, as the Tools column says it. */
export interface Failure {
  reason: HandshakeReason;
  /** The failure in words: "needs sign-in", "unreachable", ... */
  word: string;
  /** The CLI's hint, shown on hover. */
  hint: string;
}

export interface ServerRow {
  /** The row's key: the server's logical id. */
  key: string;
  name: string;
  /** Under the name: the URL of a remote server, else its command followed by its arguments. */
  address: string;
  /** The transports of its declarations, distinct, in order of first appearance. */
  transports: Transport[];
  /** How many signature versions of the server this machine has; over 1, the versions disagree. */
  versions: number;
  /** How many tools (entries of kind `tool`) the latest version exposed; undefined when never checked. */
  tools: number | undefined;
  /** When the latest version's signature was taken. */
  signatureAt: string | undefined;
  /** The configurations that declare it, in inventory order. */
  agents: ServerAgent[];
  /** The plugins that declare it, distinct. */
  plugins: string[];
  /** The failures of the last check in this session, one per distinct reason. */
  failures: Failure[];
}

/** The failure words of the CLI's human output, by `handshake_error.reason`. */
export const failureWords: Record<HandshakeReason, string> = {
  failed: "handshake failed",
  not_found: "command not found",
  timeout: "timed out",
  unauthorized: "needs sign-in",
  unreachable: "unreachable",
};

/** The signature of a server the CLI never checked. */
const NO_SIGNATURE = "none";

const distinct = <T>(items: T[]): T[] => [...new Set(items)];

const time = (at: string | undefined) => {
  if (at === undefined) {
    return Number.NEGATIVE_INFINITY;
  }
  const t = new Date(at).getTime();
  return Number.isNaN(t) ? Number.NEGATIVE_INFINITY : t;
};

/** The version checked last, or the first when none was. */
const latestOf = (nodes: McpServer[]): McpServer | undefined => {
  let best: McpServer | undefined;
  for (const node of nodes) {
    if (
      best === undefined ||
      time(node.signature_at) > time(best.signature_at)
    ) {
      best = node;
    }
  }
  return best;
};

/** The URL of a remote server, else the command and its arguments as one line. */
export const addressOf = (occurrence: ServerOccurrence | undefined) => {
  if (occurrence === undefined) {
    return "";
  }
  if (occurrence.url !== "") {
    return occurrence.url;
  }
  return [occurrence.command, ...occurrence.args].join(" ");
};

const toolCount = (node: McpServer | undefined): number | undefined =>
  node === undefined || node.signature === NO_SIGNATURE
    ? undefined
    : (node.tools ?? []).filter((t) => t.kind === "tool").length;

const agentsOf = (
  occurrences: ServerOccurrence[],
  order: string[]
): ServerAgent[] =>
  order
    .filter((id) => occurrences.some((o) => o.configuration === id))
    .map((id) => ({
      configuration: id,
      off: occurrences
        .filter((o) => o.configuration === id)
        .every((o) => o.enabled === false),
    }));

/** The failures of the declarations of one server, one per distinct reason, first hint kept. */
const failuresOf = (
  logicalId: string,
  failures: HandshakeFailure[]
): Failure[] => {
  const own = failures.filter((f) => f.logicalId === logicalId);
  return distinct(own.map((f) => f.error.reason)).map((reason) => ({
    hint: own.find((f) => f.error.reason === reason)?.error.hint ?? "",
    reason,
    word: failureWords[reason],
  }));
};

export interface ProjectionInput {
  inventory: InventoryEvent;
  handshakeFailures: HandshakeFailure[];
}

const rowOf = (
  logicalId: string,
  nodes: McpServer[],
  input: ProjectionInput
): ServerRow => {
  const latest = latestOf(nodes);
  const occurrences = nodes.flatMap((n) => n.occurrences);
  const order = input.inventory.configurations.map((c) => c.id);
  return {
    address: addressOf(latest?.occurrences[0] ?? occurrences[0]),
    agents: agentsOf(occurrences, order),
    failures: failuresOf(logicalId, input.handshakeFailures),
    key: logicalId,
    name: nodes[0]?.name ?? "",
    plugins: distinct(
      occurrences.map((o) => o.plugin).filter((p) => p !== undefined)
    ),
    signatureAt: latest?.signature_at,
    tools: toolCount(latest),
    transports: distinct(occurrences.map((o) => o.transport)),
    versions: nodes.length,
  };
};

const compareRows = (a: ServerRow, b: ServerRow) =>
  a.name.localeCompare(b.name, undefined, { sensitivity: "base" }) ||
  a.key.localeCompare(b.key);

/** One row per logical server, sorted by name. */
export const projectServers = (input: ProjectionInput): ServerRow[] => {
  const ids = distinct(input.inventory.mcp_servers.map((n) => n.logical_id));
  const rows = ids.map((id) =>
    rowOf(
      id,
      input.inventory.mcp_servers.filter((n) => n.logical_id === id),
      input
    )
  );
  rows.sort(compareRows);
  return rows;
};

/** When the last check was, over every row: the latest `signature_at`, or undefined when none. */
export const latestCheck = (rows: ServerRow[]): string | undefined => {
  let best: string | undefined;
  for (const row of rows) {
    const at = row.signatureAt;
    if (at !== undefined && (best === undefined || time(at) > time(best))) {
      best = at;
    }
  }
  return best;
};

/** The declarations whose handshake failed, from the nodes of a `scan --handshake` inventory. */
export const failuresIn = (servers: McpServer[]): HandshakeFailure[] =>
  servers.flatMap((node) =>
    node.occurrences
      .filter((o) => o.handshake_error !== undefined)
      .map((o) => ({
        configuration: o.configuration,
        error: o.handshake_error ?? { hint: "", message: "", reason: "failed" },
        logicalId: node.logical_id,
        ...(o.plugin === undefined ? {} : { plugin: o.plugin }),
      }))
  );

const plural = (n: number, one: string, many: string) =>
  `${n} ${n === 1 ? one : many}`;

/** The parts of the toast after the count, one per reason, in the order of the words table. */
const summaryParts: [HandshakeReason, string][] = [
  ["unauthorized", "need sign-in"],
  ["unreachable", "unreachable"],
  ["not_found", "command not found"],
  ["timeout", "timed out"],
  ["failed", "handshake failed"],
];

/**
 * What the toast of a check says: "Checked {n} servers · {k} need sign-in, {m} unreachable",
 * leaving out the parts that are zero, or "Checked {n} servers" when no handshake failed. Every
 * count is of servers: a server whose declarations all fail the same way counts once.
 */
export const checkSummary = (servers: McpServer[]): string => {
  const failures = failuresIn(servers);
  const checked = `Checked ${plural(distinct(servers.map((n) => n.logical_id)).length, "server", "servers")}`;
  const parts = summaryParts
    .map(([reason, words]) => ({
      count: distinct(
        failures
          .filter((f) => f.error.reason === reason)
          .map((f) => f.logicalId)
      ).length,
      words,
    }))
    .filter((p) => p.count > 0)
    .map((p) => `${p.count} ${p.words}`);
  return parts.length === 0 ? checked : `${checked} · ${parts.join(", ")}`;
};
