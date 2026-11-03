// The commands the agentx CLI takes, generated from its command tree: do
// not edit. To write it again, run in apps/cli:
//   go test ./internal/cli -run TestCommandTypes -update

// The flags of a command, each by name, true when it takes a value.
export type CliFlags = Readonly<Record<string, boolean>>;

// The flags every command takes.
export const GLOBAL_FLAGS: CliFlags = {
  color: true,
  json: false,
  verbose: false,
};

// Every command that runs something, by its words, such as "skill add",
// with its own flags.
export const CLI_COMMANDS: Readonly<Record<string, CliFlags>> = {
  adopt: {
    all: false,
    skill: true,
  },
  "config disable": {},
  "config enable": {
    "place-all": false,
  },
  "config get": {},
  "config list": {},
  "config set": {},
  doctor: {},
  export: {
    force: false,
    machine: true,
  },
  import: {
    machine: true,
    yes: false,
  },
  machine: {},
  "machine rename": {},
  "machine reset-id": {},
  scan: {
    configuration: true,
    handshake: false,
    project: true,
  },
  serve: {
    once: false,
  },
  "skill add": {
    all: false,
    copy: false,
    except: true,
    fetch: false,
    name: true,
    to: true,
  },
  "skill check-updates": {},
  "skill diff": {
    commit: true,
    update: false,
  },
  "skill files": {
    configuration: true,
  },
  "skill fork": {
    name: true,
  },
  "skill history": {},
  "skill list": {
    remote: false,
  },
  "skill new": {
    description: true,
  },
  "skill place": {
    copy: false,
    force: false,
    machine: true,
    to: true,
  },
  "skill publish": {
    message: true,
  },
  "skill remove": {
    from: true,
    machine: true,
    remote: false,
  },
  "skill rename": {},
  "skill update": {
    abort: false,
    all: false,
  },
  "source add": {
    account: false,
  },
  "source fetch": {
    all: false,
  },
  "source list": {},
  "source remove": {},
  "source skills": {},
  update: {
    check: false,
    nightly: false,
    stable: false,
    version: true,
  },
  version: {},
};
