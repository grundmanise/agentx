// The types of the events the agentx CLI writes with --json, which
// docs/spec/cli-contract.md describes. Generated from the structs the CLI
// marshals: do not edit. To write it again, run in apps/cli:
//   go test ./internal/cli -run TestEventTypes -update

// The output schema version this app reads. An event of another version is
// refused.
export const SCHEMA_VERSION = 2;

// What every event starts with.
export interface Envelope {
  type: string;
  schema_version: number;
}

// The values a field takes. An open list will gain more values, which the
// app shows as written.
export type SkillKind = "managed" | "unmanaged";
export type SkillState = "current" | "modified";
export type DriftState =
  | "displaced"
  | "missing"
  | "source removed"
  | "upstream removed"
  | "not published"
  | (string & Record<never, never>);
export type PlacementMode = "symlink" | "copy" | "library";
export type PlacementKind = "symlink" | "copy" | "directory" | "library";
export type OccurrenceKind = "symlink" | "copy" | "directory";
export type OccurrenceScope = "user" | "project";
export type Transport = "stdio" | "sse" | "streamable-http";
export type ToolKind = "tool" | "prompt" | "resource" | "resource_template";
export type HandshakeReason =
  | "unauthorized"
  | "unreachable"
  | "not_found"
  | "timeout"
  | "failed";
export type FileStatus = "added" | "modified" | "deleted";
export type DoctorStatus = "ok" | "warn" | "fail" | "info";
export type LogLevel = "warn" | "debug" | (string & Record<never, never>);
export type ErrorCode =
  | "usage"
  | "git"
  | "source"
  | "pending_merge"
  | "not_found"
  | "refused"
  | "locked"
  | "account_repo"
  | "interrupted"
  | "internal";
export type PublishOutcome =
  | "pushed"
  | "up to date"
  | "behind"
  | "moved"
  | "declined"
  | "denied"
  | "refused";
export type PullOutcome = "fast-forward" | "merged" | "conflict" | "refused";
export type ReconcileOutcome =
  | "restored"
  | "repaired"
  | "worktree missing"
  | "adopt candidate"
  | "modified"
  | "installable"
  | "unmanaged";
export type AdoptionState = "candidate" | "adopted" | "managed" | "refused";
export type ImportState = "present" | "missing" | "different";
export type ImportAction =
  | "installed"
  | "updated"
  | "placed"
  | "unplaced"
  | "kept"
  | "skipped";
export type MachineDerivation = "platform" | "random";
export type SourceAccess = "writable" | "read-only" | "unknown";
export type ProgressPhase =
  | "fetch"
  | "publish"
  | "blobs"
  | "import"
  | "install"
  | "place"
  | "adopt"
  | "rescan";

// The exit code of each error code, the ok of success included.
export const EXIT_CODES = {
  account_repo: 8,
  git: 2,
  internal: 10,
  interrupted: 9,
  locked: 7,
  not_found: 5,
  ok: 0,
  pending_merge: 4,
  refused: 6,
  source: 3,
  usage: 1,
} as const;
export type ExitCode = (typeof EXIT_CODES)[keyof typeof EXIT_CODES];

// The objects events carry.
export interface Settings {
  schema_version: number;
  label?: string;
  accept_operations: boolean;
  ignore_system_files: boolean;
  disabled_configurations: string[];
  sources: SettingsSource[];
  copy_mode: Record<string, string[]>;
}

export interface LibraryEntry {
  name: string;
  kind: SkillKind;
  fork_id?: string;
  source?: string;
  subpath?: string;
  upstream?: string;
  upstream_subpath?: string;
  upstream_commit?: string;
  base_hash?: string;
  content_hash: string;
  state?: SkillState;
  drift?: DriftState[];
  candidate?: LibraryCandidate;
  pending_merge?: boolean;
  placements: LibraryPlacement[];
  universal: string[];
}

export interface ChangedFile {
  path: string;
  status: FileStatus;
}

export interface ConflictFile {
  path: string;
  base: string | null;
  mine: string | null;
  theirs: string | null;
}

export interface SnapshotSkill {
  name: string;
  kind: SkillKind;
  fork_id?: string;
  commit?: string;
  source?: string;
  subpath?: string;
  upstream?: string;
  upstream_subpath?: string;
  upstream_commit?: string;
  base_hash?: string;
  placements: string[];
}

export interface Inventory {
  instance_id: string;
  scan_counter: number;
  machine: Machine;
  configurations: Configuration[];
  skills: SkillNode[];
  library: LibraryEntry[];
  mcp_servers: McpServer[];
  plugins: Plugin[];
  edges: Edge[];
  warnings: string[];
}

export interface SearchResult {
  source: string;
  subpath: string;
  name: string;
  description: string;
  tree: string;
}

export interface SettingsSource {
  url: string;
  alias?: string;
  account?: boolean;
  pin?: string;
  access?: SourceAccess;
  access_checked?: string;
  default_branch?: string;
  last_fetched?: string;
}

export interface LibraryCandidate {
  upstream_commit: string;
  content_hash: string;
}

export interface LibraryPlacement {
  configuration: string;
  path: string;
  mode: PlacementMode;
  kind: PlacementKind;
}

export interface Machine {
  id: string;
  label: string;
}

export interface Configuration {
  id: string;
  client: string;
  name: string;
  path: string;
  enabled: boolean;
  reads_library: boolean;
  physical_id: string;
  logical_id: string;
}

export interface SkillNode {
  physical_id: string;
  logical_id: string;
  name: string;
  description: string;
  content_hash: string;
  occurrences: Occurrence[];
}

export interface McpServer {
  physical_id: string;
  logical_id: string;
  name: string;
  signature: string;
  signature_at?: string;
  tools?: McpTool[];
  occurrences: ServerOccurrence[];
}

export interface Plugin {
  physical_id: string;
  logical_id: string;
  name: string;
  marketplace: string;
  version: string;
  configuration: string;
  path: string;
  enabled?: boolean;
}

export interface Edge {
  from: string;
  to: string;
}

export interface Occurrence {
  id: string;
  configuration: string;
  path: string;
  resolved_path: string;
  kind: OccurrenceKind;
  scope: OccurrenceScope;
  plugin?: string;
}

export interface McpTool {
  id: string;
  kind: ToolKind;
  name: string;
  description: string;
}

export interface ServerOccurrence {
  id: string;
  configuration: string;
  config_file: string;
  command: string;
  args: string[];
  env_keys: string[];
  url: string;
  header_keys: string[];
  transport: Transport;
  handshake: boolean;
  handshake_error?: HandshakeError;
  plugin?: string;
  enabled?: boolean;
}

export interface HandshakeError {
  reason: HandshakeReason;
  message: string;
  hint: string;
}

// One type per event type.
export interface VersionEvent {
  type: "version";
  schema_version: number;
  cli_version: string;
}

export interface SettingsEvent {
  type: "settings";
  schema_version: number;
  settings: Settings;
}

export interface MachineEvent {
  type: "machine";
  schema_version: number;
  id: string;
  label: string;
  derivation: MachineDerivation;
}

export interface SourceEvent {
  type: "source";
  schema_version: number;
  id: string;
  url: string;
  alias?: string;
  account?: boolean;
  pin?: string;
  access?: SourceAccess;
  access_checked?: string;
  access_reason?: string;
  default_branch?: string;
  subpath?: string;
  last_fetched?: string;
  commit?: string;
  previous_commit?: string;
  skills?: number;
}

export interface SourceSkillEvent {
  type: "source_skill";
  schema_version: number;
  source: string;
  subpath: string;
  name: string;
  description: string;
  tree: string;
}

export interface ProgressEvent {
  type: "progress";
  schema_version: number;
  phase: ProgressPhase;
  subject?: string;
  current: number;
  total?: number;
}

export interface LibrarySkillEvent extends LibraryEntry {
  type: "library_skill";
  schema_version: number;
}

export interface DiffEvent {
  type: "diff";
  schema_version: number;
  name: string;
  path: string;
  status: FileStatus;
  patch: string;
}

export interface SkillFileEvent {
  type: "skill_file";
  schema_version: number;
  name: string;
  path: string;
  size: number;
  binary: boolean;
  lines?: number;
  content?: string;
  truncated?: boolean;
  ignored?: boolean;
}

export interface HistoryEvent {
  type: "history";
  schema_version: number;
  name: string;
  commit: string;
  parents: string[];
  time: string;
  author: string;
  subject: string;
  message: string;
  machine?: string;
  import: boolean;
  foreign: boolean;
  files: ChangedFile[];
}

export interface ForkNoteEvent {
  type: "fork_note";
  schema_version: number;
  name: string;
  plugin: string;
  configuration: string;
  note: string;
}

export interface UpdateAvailableEvent {
  type: "update_available";
  schema_version: number;
  instance_id?: string;
  name: string;
  kind: SkillKind;
  source?: string;
  subpath?: string;
  upstream?: string;
  upstream_subpath?: string;
  base_hash: string;
  upstream_commit: string;
  candidate: string;
  candidate_hash: string;
  candidate_upstream_commit: string;
  files: ChangedFile[];
  upstream_name?: string;
}

export interface ConflictEvent {
  type: "conflict";
  schema_version: number;
  name: string;
  kind: SkillKind;
  base: string;
  mine: string;
  theirs: string;
  files: ConflictFile[];
}

export interface ExportEvent {
  type: "export";
  schema_version: number;
  path?: string;
  machine?: string;
  commit?: string;
  skills: number;
}

export interface ImportSkillEvent extends SnapshotSkill {
  type: "import_skill";
  schema_version: number;
  state: ImportState;
  local_commit?: string;
  action: ImportAction;
  reason?: string;
}

export interface MachineSkillEvent {
  type: "machine_skill";
  schema_version: number;
  machine: string;
  name: string;
  placements: string[];
  removed: boolean;
  commit: string;
}

export interface PullEvent {
  type: "pull";
  schema_version: number;
  name: string;
  outcome: PullOutcome;
  commit?: string;
  base?: string;
}

export interface PublishEvent {
  type: "publish";
  schema_version: number;
  name: string;
  outcome: PublishOutcome;
  commit?: string;
  source: string;
  branch?: string;
}

export interface InstallableSkillEvent {
  type: "installable_skill";
  schema_version: number;
  name: string;
  fork_id: string;
  commit: string;
  source: string;
  upstream?: string;
  upstream_subpath?: string;
  upstream_commit?: string;
  base_hash?: string;
}

export interface ErrorEvent {
  type: "error";
  schema_version: number;
  code: ErrorCode;
  message: string;
  hint?: string;
}

export interface ResultEvent {
  type: "result";
  schema_version: number;
  ok: boolean;
  summary?: string;
}

export interface LogEvent {
  type: "log";
  schema_version: number;
  level: LogLevel;
  message: string;
}

export interface InventoryEvent extends Inventory {
  type: "inventory";
  schema_version: number;
}

export interface DoctorEvent {
  type: "doctor";
  schema_version: number;
  check: string;
  status: DoctorStatus;
  detail: string;
  hint?: string;
}

export interface AdoptionEvent {
  type: "adoption";
  schema_version: number;
  name: string;
  lock: string;
  source?: string;
  subpath?: string;
  state: AdoptionState;
  upstream_commit?: string;
  base_hash?: string;
  content_hash?: string;
  modified?: boolean;
  restored?: string[];
  reason?: string;
}

export interface ReconcileEvent {
  type: "reconcile";
  schema_version: number;
  name: string;
  kind: SkillKind;
  outcome: ReconcileOutcome;
  path?: string;
}

export interface DriftEvent {
  type: "drift";
  schema_version: number;
  instance_id: string;
  scan_counter: number;
  name: string;
  kind: SkillKind;
  state?: SkillState;
  drift: DriftState[];
  previous_state?: SkillState;
  previous_drift: DriftState[];
}

export interface RefreshCompleteEvent {
  type: "refresh_complete";
  schema_version: number;
  request_id: string;
  instance_id: string;
  ok: boolean;
  scan_counter?: number;
  error?: string;
}

export interface SearchEvent {
  type: "search";
  schema_version: number;
  request_id: string;
  instance_id: string;
  query: string;
  results: SearchResult[];
}

// Reserved by the contract and never emitted: known, and not rendered.
export interface OperationEvent {
  type: "operation";
  schema_version: number;
}
export interface FleetEvent {
  type: "fleet";
  schema_version: number;
}

// Every event type the app knows.
export type CliEvent =
  | VersionEvent
  | SettingsEvent
  | MachineEvent
  | SourceEvent
  | SourceSkillEvent
  | ProgressEvent
  | LibrarySkillEvent
  | DiffEvent
  | SkillFileEvent
  | HistoryEvent
  | ForkNoteEvent
  | UpdateAvailableEvent
  | ConflictEvent
  | ExportEvent
  | ImportSkillEvent
  | MachineSkillEvent
  | PullEvent
  | PublishEvent
  | InstallableSkillEvent
  | ErrorEvent
  | ResultEvent
  | LogEvent
  | InventoryEvent
  | DoctorEvent
  | AdoptionEvent
  | ReconcileEvent
  | DriftEvent
  | RefreshCompleteEvent
  | SearchEvent
  | OperationEvent
  | FleetEvent;
export type CliEventType = CliEvent["type"];
export type EventOf<T extends CliEventType> = Extract<CliEvent, { type: T }>;

// The requests serve reads on its stdin, one JSON object a line.
export interface RefreshRequest {
  type: "refresh";
  request_id: string;
}
export interface SearchRequest {
  type: "search";
  request_id: string;
  query: string;
}
export type ServeRequest = RefreshRequest | SearchRequest;
