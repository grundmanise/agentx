// The building blocks of the presets. Files other tools own are written here as plain files and
// git; agentx's own state is made only by running agentx.
import { spawnSync } from "node:child_process";
import { cpSync, mkdirSync, symlinkSync, writeFileSync } from "node:fs";
import path from "node:path";

import { agents } from "./agents.ts";
import type { AgentId } from "./agents.ts";

/** Where a preset is built: the HOME it fills, the folder of its bare repos, and the agentx. */
export interface PresetPaths {
  home: string;
  remotes: string;
  cli: string;
}

/** A preset being built: its paths and the lines of its summary. */
export interface Machine extends PresetPaths {
  made: string[];
}

/** The date of every commit, unless a step names a later one. */
const firstDate = "2026-10-01T09:00:00Z";
/** The date of an upstream change made after the skills were installed. */
export const laterDate = "2026-10-15T09:00:00Z";

/** The git identity of the machine's user and of every commit in the bare repos. */
const author = { email: "dev@example.com", name: "Dev" };

/** `${name}`, a variable an agent expands in an MCP server declaration. */
export const variable = (name: string): string => `\${${name}}`;

/** The host the source URLs name; git rewrites it to the bare repos under remotes. */
const githubBase = "https://github.com/";

export interface Skill {
  name: string;
  description: string;
  /** The Markdown under the frontmatter; a heading and the description when left out. */
  body?: string;
  /** More files of the skill folder, by path inside it. */
  files?: Record<string, string>;
  /** The skill's folder in its repo; skills/<name> when left out. */
  folder?: string;
}

/** A bare repo standing in for a repository on GitHub, and the folder its commits are made in. */
export interface Remote {
  url: string;
  bare: string;
  work: string;
}

export const machine = (paths: PresetPaths): Machine => ({
  ...paths,
  made: [],
});

const plainYaml = /^[\p{L}\p{N}][^\n:#"'\\]*$/u;

/** A YAML scalar: plain when that reads the same, else double quoted. */
const yamlString = (value: string): string =>
  plainYaml.test(value) && value === value.trim()
    ? value
    : JSON.stringify(value);

const skillMd = (skill: Skill): string =>
  [
    "---",
    `name: ${yamlString(skill.name)}`,
    `description: ${yamlString(skill.description)}`,
    "---",
    "",
    skill.body ?? `# ${skill.name}\n\n${skill.description}\n`,
  ].join("\n");

const writeText = (file: string, text: string): void => {
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, text);
};

const writeJson = (file: string, value: unknown): void => {
  writeText(file, `${JSON.stringify(value, null, 2)}\n`);
};

const writeFiles = (root: string, files: Record<string, string>): void => {
  for (const [name, text] of Object.entries(files)) {
    writeText(path.join(root, name), text);
  }
};

/** The files of a skill folder, by path inside it. */
const skillFiles = (skill: Skill): Record<string, string> => ({
  "SKILL.md": skillMd(skill),
  ...skill.files,
});

const writeSkill = (dir: string, skill: Skill): void => {
  writeFiles(dir, skillFiles(skill));
};

const skillFolder = (skill: Skill): string =>
  skill.folder ?? `skills/${skill.name}`;

const run = (
  command: string,
  args: string[],
  env: NodeJS.ProcessEnv
): string => {
  const result = spawnSync(command, args, { encoding: "utf-8", env });
  if (result.error !== undefined) {
    throw result.error;
  }
  if (result.status !== 0) {
    throw new Error(
      `${command} ${args.join(" ")} exited ${result.status}\n${result.stdout}${result.stderr}`
    );
  }
  return result.stdout;
};

/** The process environment without what would point agentx or git elsewhere. */
const baseEnv = (): NodeJS.ProcessEnv =>
  Object.fromEntries(
    Object.entries(process.env).filter(
      ([key]) =>
        !["AGENTX_", "XDG_", "GIT_"].some((prefix) => key.startsWith(prefix)) &&
        key !== "COPILOT_HOME" &&
        key !== "SKILLSHARE_CONFIG"
    )
  );

const dates = (date: string) => ({
  GIT_AUTHOR_DATE: date,
  GIT_COMMITTER_DATE: date,
});

/** Runs git with no user or system configuration, as the author of the bare repos. */
const git = (args: string[], date = firstDate): string =>
  run("git", args, {
    ...baseEnv(),
    ...dates(date),
    GIT_AUTHOR_EMAIL: author.email,
    GIT_AUTHOR_NAME: author.name,
    GIT_COMMITTER_EMAIL: author.email,
    GIT_COMMITTER_NAME: author.name,
    GIT_CONFIG_GLOBAL: "/dev/null",
    GIT_CONFIG_NOSYSTEM: "1",
  });

/** Runs agentx on the machine's HOME, with fixed commit dates. */
export const agentx = (m: Machine, ...args: string[]): string =>
  run(m.cli, args, { ...baseEnv(), ...dates(firstDate), HOME: m.home });

/**
 * Writes ~/.gitconfig: the user's identity, and a rewrite of GitHub URLs to the bare repos, so
 * that sources carry GitHub URLs and every fetch and push stays on this machine.
 */
export const gitConfig = (m: Machine): void => {
  writeText(
    path.join(m.home, ".gitconfig"),
    [
      "[user]",
      `\tname = ${author.name}`,
      `\temail = ${author.email}`,
      `[url "file://${m.remotes}/"]`,
      `\tinsteadOf = ${githubBase}`,
      "",
    ].join("\n")
  );
};

/** Starts agentx on the machine: telemetry off and a neutral label. */
export const settle = (m: Machine): void => {
  agentx(m, "config", "set", "telemetry", "off");
  agentx(m, "machine", "rename", "laptop");
};

/** Creates an agent's configuration folder, which is what agentx detects it by. */
export const addAgents = (m: Machine, ids: AgentId[]): void => {
  for (const id of ids) {
    mkdirSync(path.join(m.home, agents[id].config), { recursive: true });
  }
  m.made.push(`${ids.length} agents: ${ids.join(", ")}`);
};

const agentPath = (m: Machine, id: AgentId, ...rest: string[]) =>
  path.join(m.home, agents[id].config, ...rest);

/** Writes skills into an agent's own skills folder, where no tool manages them. */
export const unmanagedSkills = (
  m: Machine,
  id: AgentId,
  skills: Skill[]
): void => {
  for (const skill of skills) {
    writeSkill(path.join(m.home, agents[id].skills, skill.name), skill);
  }
  m.made.push(`${skills.length} unmanaged skills in ${id}`);
};

/** Creates an empty bare repo for owner/repo on GitHub, with its work folder under remotes/.work. */
const remote = (m: Machine, slug: string): Remote => {
  const r = {
    bare: path.join(m.remotes, `${slug}.git`),
    url: `${githubBase}${slug}`,
    work: path.join(m.remotes, ".work", slug),
  };
  git(["init", "--quiet", "--bare", "--initial-branch=main", r.bare]);
  mkdirSync(r.work, { recursive: true });
  return r;
};

/** Writes files into the repo's work folder and commits them all to main. */
export const commit = (
  r: Remote,
  files: Record<string, string>,
  message: string,
  date = firstDate
): void => {
  writeFiles(r.work, files);
  const repo = [`--git-dir=${r.bare}`, `--work-tree=${r.work}`];
  git([...repo, "add", "--all"]);
  git([...repo, "commit", "--quiet", "--message", message], date);
};

/** The files of skills laid out in their folders of a repo. */
export const skillTree = (skills: Skill[]): Record<string, string> =>
  Object.fromEntries(
    skills.flatMap((skill) =>
      Object.entries(skillFiles(skill)).map(([name, text]) => [
        `${skillFolder(skill)}/${name}`,
        text,
      ])
    )
  );

/** A source repo on GitHub holding skills, in one commit. */
export const sourceRepo = (
  m: Machine,
  slug: string,
  skills: Skill[]
): Remote => {
  const r = remote(m, slug);
  commit(
    r,
    {
      "README.md": `# ${slug}\n\nSkills for coding agents.\n`,
      ...skillTree(skills),
    },
    "Add skills"
  );
  m.made.push(`source ${r.url}: ${skills.length} skills`);
  return r;
};

/**
 * Installs skills the way the Vercel skills CLI does: a copy of each in the library, a relative
 * symlink in each agent's skills folder, and their entries in ~/.agents/.skill-lock.json.
 */
export const skillsCliInstall = (
  m: Machine,
  r: Remote,
  skills: Skill[],
  into: AgentId[]
): void => {
  const library = path.join(m.home, ".agents", "skills");
  const entries: Record<string, object> = {};
  for (const skill of skills) {
    const folder = skillFolder(skill);
    cpSync(path.join(r.work, folder), path.join(library, skill.name), {
      recursive: true,
    });
    for (const id of into) {
      const dir = path.join(m.home, agents[id].skills);
      mkdirSync(dir, { recursive: true });
      symlinkSync(
        path.relative(dir, path.join(library, skill.name)),
        path.join(dir, skill.name)
      );
    }
    entries[skill.name] = {
      installedAt: firstDate,
      skillFolderHash: git([
        `--git-dir=${r.bare}`,
        "rev-parse",
        `HEAD:${folder}`,
      ]).trim(),
      skillPath: `${folder}/SKILL.md`,
      source: r.url.slice(githubBase.length),
      sourceType: "github",
      sourceUrl: `${r.url}.git`,
      updatedAt: firstDate,
    };
  }
  writeJson(path.join(m.home, ".agents", ".skill-lock.json"), {
    skills: entries,
    version: 3,
  });
  m.made.push(
    `installed by the skills CLI from ${r.url}: ${skills.map((skill) => skill.name).join(", ")}`
  );
};

/** Claude Code's user-scope MCP servers, in ~/.claude.json. */
export const claudeServers = (m: Machine, servers: Record<string, object>) => {
  writeJson(path.join(m.home, ".claude.json"), {
    mcpServers: servers,
    numStartups: 42,
  });
  m.made.push(`${Object.keys(servers).length} MCP servers in claude-code`);
};

/** Cursor's MCP servers, in ~/.cursor/mcp.json. */
export const cursorServers = (m: Machine, servers: Record<string, object>) => {
  writeJson(agentPath(m, "cursor", "mcp.json"), { mcpServers: servers });
  m.made.push(`${Object.keys(servers).length} MCP servers in cursor`);
};

/** Codex's config.toml, with its MCP servers given as TOML tables. */
export const codexConfig = (m: Machine, servers: Record<string, string>) => {
  const tables = Object.entries(servers).map(
    ([name, body]) => `[mcp_servers.${JSON.stringify(name)}]\n${body}\n`
  );
  writeText(
    agentPath(m, "codex", "config.toml"),
    ['model = "gpt-5-codex"', "", ...tables].join("\n")
  );
  m.made.push(`${tables.length} MCP servers in codex`);
};

export interface ClaudePlugin {
  name: string;
  marketplace: string;
  version: string;
  description: string;
  enabled: boolean;
  skills: Skill[];
  servers?: Record<string, object>;
}

/**
 * Installs Claude Code plugins as Claude Code does: each bundle in the plugin cache, the install
 * records, the enabled state in settings.json, and the marketplaces' GitHub repos.
 */
export const claudePlugins = (m: Machine, plugins: ClaudePlugin[]): void => {
  const records: Record<string, object[]> = {};
  const enabled: Record<string, boolean> = {};
  const marketplaces: Record<string, object> = {};
  for (const p of plugins) {
    const key = `${p.name}@${p.marketplace}`;
    const dir = agentPath(
      m,
      "claude-code",
      "plugins",
      "cache",
      p.marketplace,
      p.name,
      p.version
    );
    writeJson(path.join(dir, ".claude-plugin", "plugin.json"), {
      description: p.description,
      name: p.name,
      version: p.version,
    });
    for (const skill of p.skills) {
      writeSkill(path.join(dir, "skills", skill.name), skill);
    }
    if (p.servers !== undefined) {
      writeJson(path.join(dir, ".mcp.json"), { mcpServers: p.servers });
    }
    records[key] = [
      {
        installPath: dir,
        installedAt: firstDate,
        scope: "user",
        version: p.version,
      },
    ];
    enabled[key] = p.enabled;
    marketplaces[p.marketplace] = {
      source: { repo: `${p.marketplace}/claude-plugins`, source: "github" },
    };
    m.made.push(`plugin ${key} with ${p.skills.length} skills`);
  }
  writeJson(agentPath(m, "claude-code", "plugins", "installed_plugins.json"), {
    plugins: records,
    version: 2,
  });
  writeJson(
    agentPath(m, "claude-code", "plugins", "known_marketplaces.json"),
    marketplaces
  );
  writeJson(agentPath(m, "claude-code", "settings.json"), {
    enabledPlugins: enabled,
  });
};

/** Overwrites the SKILL.md of a skill in the library, as a person editing it does. */
export const editSkill = (m: Machine, skill: Skill): void => {
  writeText(
    path.join(m.home, ".agents", "skills", skill.name, "SKILL.md"),
    skillMd(skill)
  );
};

/** Creates an empty bare repo and adds it as the hub, the account remote hub skills go to. */
export const hub = (m: Machine, slug: string): Remote => {
  const r = remote(m, slug);
  agentx(m, "source", "add", r.url, "--hub");
  m.made.push(`hub ${r.url}`);
  return r;
};

/** Creates a hub skill from agentx's template, then writes its content as a person does. */
export const newHubSkill = (m: Machine, skill: Skill): void => {
  agentx(m, "skill", "new", skill.name);
  editSkill(m, skill);
};
