// Machine presets: named recipes that build a home folder that looks like a real user's machine,
// for screenshots and for tests against the real `agentx serve`. Each starts from an empty HOME
// and runs offline: sources and the hub are bare repos under `remotes`.
import { heavy } from "./heavy.ts";
import { hostile } from "./hostile.ts";
import { gitConfig, machine, settle } from "./kit.ts";
import type { Machine, PresetPaths } from "./kit.ts";
import { typical } from "./typical.ts";

export type { PresetPaths } from "./kit.ts";

/** First launch: no agent detected and nothing installed. */
const empty = (m: Machine): void => {
  m.made.push("no agents, no skills");
};

const presets = { empty, heavy, hostile, typical } as const;

export type PresetName = keyof typeof presets;

export const isPresetName = (name: string): name is PresetName =>
  Object.hasOwn(presets, name);

export const presetNames: PresetName[] =
  Object.keys(presets).filter(isPresetName);

/** Builds the preset into paths.home and returns the lines of its summary. */
export const buildPreset = (name: PresetName, paths: PresetPaths): string[] => {
  const m = machine(paths);
  gitConfig(m);
  settle(m);
  presets[name](m);
  return m.made;
};
