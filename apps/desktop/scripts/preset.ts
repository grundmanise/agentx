// Builds a machine preset (e2e/presets) into $HOME, for screenshots and tests against the real
// `agentx serve`:
//
//   node scripts/preset.ts <name>
//
// The bare repos of its sources and hub go in $AGENTX_REMOTES, /srv/remotes when unset, and the
// agentx it runs is $AGENTX_CLI, `agentx` on PATH when unset. It writes into the HOME it is
// given, so it runs only in the container, whose image creates /.agentx-container.
import { existsSync, mkdirSync, readdirSync } from "node:fs";
import path from "node:path";

import {
  buildPreset,
  isPresetName,
  presetNames,
} from "../e2e/presets/index.ts";

const fail = (message: string): never => {
  process.stderr.write(`preset: ${message}\n`);
  process.exit(1);
};

const name = process.argv.at(2) ?? "";
if (!isPresetName(name)) {
  process.stderr.write(
    `usage: node scripts/preset.ts ${presetNames.join("|")}\n`
  );
  process.exit(2);
}
if (!existsSync("/.agentx-container")) {
  fail("runs only in the agentx container (/.agentx-container is missing)");
}

/** An environment variable, or fallback when it is unset or empty. */
const env = (key: string, fallback: string): string => {
  const value = process.env[key];
  return value === undefined || value === "" ? fallback : value;
};

const home = env("HOME", "");
if (home === "") {
  fail("HOME is not set");
}
const remotes = path.resolve(env("AGENTX_REMOTES", "/srv/remotes"));
const cli = env("AGENTX_CLI", "agentx");
if (existsSync(path.join(home, ".agentx"))) {
  fail(`${home} already holds an agentx home`);
}
if (remotes.startsWith(path.join(home, path.sep)) || remotes === home) {
  fail(`the remotes folder ${remotes} must be outside HOME`);
}
mkdirSync(remotes, { recursive: true });
if (readdirSync(remotes).length > 0) {
  fail(`the remotes folder ${remotes} is not empty`);
}

const started = performance.now();
const made = buildPreset(name, { cli, home, remotes });
const seconds = ((performance.now() - started) / 1000).toFixed(1);
process.stdout.write(
  [
    `preset ${name} built in ${seconds} s`,
    `  home     ${home}`,
    `  remotes  ${remotes}`,
    ...made.map((line) => `  ${line}`),
    "",
  ].join("\n")
);
