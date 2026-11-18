/**
 * How far a progress rim shows a command's run (DESIGN.md §8 "Progress rim"). A CLI reports its
 * steps in bursts, such as three per skill and one rescan for `skill add`, so the rim never shows
 * them as they come: it creeps toward 90% on its own, as a browser's loading bar does, and the steps only raise
 * the floor it eases toward, at a capped speed, so it never jumps. When the run ends, the rim
 * eases into 100% in a fixed time, its two arcs join, it holds, and it fades.
 */

/** The run the rim shows: the share of the CLI's steps that ended, and how the run stands. */
export interface RimRun {
  state: "running" | "done" | "stopped";
  /** The steps that ended, out of the steps of the whole run, 0 to 1. */
  fraction: number;
}

/** What the rim draws, and where its run stands. */
export interface RimFrame {
  /** How far the arcs reach, 0 to 1. */
  progress: number;
  /** Where the creep is; `progress` follows it, smoothed once more. */
  raw: number;
  /** How strongly it draws, 0 to 1. */
  alpha: number;
  /** 0 while the run goes on; eases to 1 as the two arcs join at the end. */
  settle: number;
  /** Seconds since the run started. */
  elapsed: number;
  /** When the run ended, in `elapsed` seconds, and how far the rim was then. */
  doneAt?: number;
  doneFrom?: number;
  /** The run ended and the rim has faded. */
  finished: boolean;
}

export const RIM_START: RimFrame = {
  alpha: 0,
  elapsed: 0,
  finished: false,
  progress: 0,
  raw: 0,
  settle: 0,
};

/** Where the creep starts, so the rim shows at once. */
const SLIVER = 0.035;
/** What the creep and the steps reach before the run ends. */
const CEILING = 0.94;
/** How long the creep takes to cover most of its way, in seconds. */
const CREEP = 5.5;
/** The fastest the rim grows while the run goes on, in share per second. */
const MAX_SPEED = 0.16;
/** How long the rim takes to fade in, in seconds. */
const FADE_IN = 0.5;
/** The end, in seconds after the run ended: the arcs reach 100%, join, hold and fade. */
const FILL = 1.2;
const JOIN_FROM = 0.6;
const JOIN = 0.9;
const FADE_FROM = 1.85;
const FADE = 1.1;

const clamp01 = (x: number) => Math.min(1, Math.max(0, x));
const ease = (rate: number, dt: number) => 1 - Math.exp(-rate * dt);
/** Starts at about the speed the rim had, and slows into the end. */
const easeOut = (x: number) => 1 - (1 - clamp01(x)) ** 2;
/** Smootherstep: no jump in speed at either end. */
const smoother = (x: number) => {
  const k = clamp01(x);
  return k * k * k * (k * (k * 6 - 15) + 10);
};

/** The next frame, `dt` seconds after `prev`. */
export const stepRim = (prev: RimFrame, run: RimRun, dt: number): RimFrame => {
  const elapsed = prev.elapsed + dt;
  if (run.state === "stopped") {
    // Drains back and fades.
    const alpha = Math.max(0, prev.alpha - 1.6 * dt);
    const progress = prev.progress * (1 - ease(3, dt));
    return {
      ...prev,
      alpha,
      elapsed,
      finished: alpha <= 0,
      progress,
      raw: progress,
      settle: 0,
    };
  }
  if (run.state === "done") {
    const doneAt = prev.doneAt ?? elapsed;
    const doneFrom = prev.doneFrom ?? prev.progress;
    const since = elapsed - doneAt;
    const fade = (since - FADE_FROM) / FADE;
    const progress = doneFrom + (1 - doneFrom) * easeOut(since / FILL);
    return {
      alpha:
        fade > 0 ? 1 - smoother(fade) : Math.min(1, prev.alpha + dt / FADE_IN),
      doneAt,
      doneFrom,
      elapsed,
      finished: fade >= 1,
      progress,
      raw: progress,
      settle: smoother((since - JOIN_FROM) / JOIN),
    };
  }
  const creep = SLIVER + (0.9 - SLIVER) * (1 - Math.exp(-elapsed / CREEP));
  const floor = 0.9 * clamp01(run.fraction) + 0.03;
  const target = Math.min(CEILING, Math.max(creep, floor));
  const grow = Math.min((target - prev.raw) * ease(1.3, dt), MAX_SPEED * dt);
  const raw = Math.max(
    prev.raw + Math.max(0, grow),
    Math.min(SLIVER, prev.raw + 0.6 * dt)
  );
  return {
    alpha: Math.min(1, prev.alpha + dt / FADE_IN),
    elapsed,
    finished: false,
    progress: prev.progress + (raw - prev.progress) * ease(4, dt),
    raw,
    settle: 0,
  };
};
