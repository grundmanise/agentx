// The commands the agentx CLI takes, generated from its command tree: do
// not edit. To write it again, run in apps/cli:
//   go test ./internal/cli -run TestCommandTypes -update

// One command: how many arguments it takes and its flags.
export interface CliCommand {
  // The fewest and the most arguments; null when it takes any number.
  args: readonly [number, number | null];
  // Each flag by name, true when it takes a value.
  flags: Readonly<Record<string, boolean>>;
}

// The flags every command takes, true when one takes a value.
export const GLOBAL_FLAGS: Readonly<Record<string, boolean>> = {
  color: true,
  json: false,
  verbose: false,
};

// Every command that runs something, by its words, such as "skill add".
export const CLI_COMMANDS: Readonly<Record<string, CliCommand>> = {
  adopt: {
    args: [0, 0],
    flags: {
      all: false,
      base: true,
      skill: true,
    },
  },
  "config disable": {
    args: [1, 1],
    flags: {},
  },
  "config enable": {
    args: [1, 1],
    flags: {
      "place-all": false,
    },
  },
  "config get": {
    args: [1, 1],
    flags: {},
  },
  "config list": {
    args: [0, 0],
    flags: {},
  },
  "config set": {
    args: [2, 2],
    flags: {},
  },
  doctor: {
    args: [0, 0],
    flags: {},
  },
  export: {
    args: [1, 1],
    flags: {
      force: false,
    },
  },
  import: {
    args: [1, 1],
    flags: {
      yes: false,
    },
  },
  machine: {
    args: [0, 0],
    flags: {},
  },
  "machine rename": {
    args: [1, 1],
    flags: {},
  },
  "machine reset-id": {
    args: [0, 0],
    flags: {},
  },
  scan: {
    args: [0, 0],
    flags: {
      configuration: true,
      handshake: false,
      project: true,
    },
  },
  serve: {
    args: [0, 0],
    flags: {
      once: false,
    },
  },
  "skill add": {
    args: [0, 1],
    flags: {
      all: false,
      copy: false,
      except: true,
      fetch: false,
      name: true,
      to: true,
    },
  },
  "skill check-updates": {
    args: [0, 0],
    flags: {},
  },
  "skill diff": {
    args: [1, 1],
    flags: {
      commit: true,
      update: false,
    },
  },
  "skill fork": {
    args: [1, 1],
    flags: {
      name: true,
    },
  },
  "skill history": {
    args: [1, 1],
    flags: {},
  },
  "skill list": {
    args: [0, 0],
    flags: {
      remote: false,
    },
  },
  "skill new": {
    args: [1, 1],
    flags: {
      description: true,
    },
  },
  "skill place": {
    args: [1, 1],
    flags: {
      copy: false,
      force: false,
      to: true,
    },
  },
  "skill publish": {
    args: [0, 1],
    flags: {
      message: true,
    },
  },
  "skill remove": {
    args: [1, 1],
    flags: {
      from: true,
      remote: false,
    },
  },
  "skill rename": {
    args: [2, 2],
    flags: {},
  },
  "skill update": {
    args: [0, 1],
    flags: {
      abort: false,
      all: false,
    },
  },
  "source add": {
    args: [1, 1],
    flags: {
      account: false,
    },
  },
  "source fetch": {
    args: [0, null],
    flags: {
      all: false,
    },
  },
  "source list": {
    args: [0, 0],
    flags: {},
  },
  "source remove": {
    args: [1, 1],
    flags: {},
  },
  "source skills": {
    args: [1, 1],
    flags: {},
  },
  version: {
    args: [0, 0],
    flags: {},
  },
};
