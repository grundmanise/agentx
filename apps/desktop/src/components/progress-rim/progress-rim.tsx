import { useEffect, useEffectEvent, useLayoutEffect, useRef } from "react";

import { RIM_START, stepRim } from "#/components/progress-rim/rim-progress.ts";
import type {
  RimFrame,
  RimRun,
} from "#/components/progress-rim/rim-progress.ts";
import {
  RIM_FRAGMENT,
  RIM_VERTEX,
} from "#/components/progress-rim/rim-shader.ts";

/** Each size: the room around the pill for the glow, in px; how wide the glow and head are; how bright the glow is. */
const SIZES = {
  md: { pad: 26, scale: 1, strength: 1 },
  sm: { pad: 14, scale: 0.45, strength: 0.55 },
} as const;

/** A colour token's value as the shader's RGB, 0 to 1; `fallback` when it is no hex colour. */
const tokenRgb = (
  element: Element,
  token: string,
  fallback: [number, number, number]
): [number, number, number] => {
  const value = getComputedStyle(element).getPropertyValue(token).trim();
  const hex = /^#(?<hex>[0-9a-f]{6})$/iu.exec(value)?.groups?.hex;
  if (hex === undefined) {
    return fallback;
  }
  const n = Number.parseInt(hex, 16);
  // oxlint-disable-next-line no-bitwise -- unpacks the three bytes of a hex colour
  return [((n >> 16) & 255) / 255, ((n >> 8) & 255) / 255, (n & 255) / 255];
};

interface Program {
  gl: WebGLRenderingContext;
  uniform: (name: string) => WebGLUniformLocation | null;
}

const compile = (canvas: HTMLCanvasElement): Program | undefined => {
  const gl = canvas.getContext("webgl", {
    alpha: true,
    premultipliedAlpha: true,
  });
  if (gl === null) {
    return undefined;
  }
  const program = gl.createProgram();
  for (const [type, source] of [
    [gl.VERTEX_SHADER, RIM_VERTEX],
    [gl.FRAGMENT_SHADER, RIM_FRAGMENT],
  ] as const) {
    const shader = gl.createShader(type);
    if (shader === null) {
      return undefined;
    }
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    gl.attachShader(program, shader);
  }
  gl.linkProgram(program);
  if (gl.getProgramParameter(program, gl.LINK_STATUS) !== true) {
    return undefined;
  }
  gl.useProgram(program);
  gl.bindBuffer(gl.ARRAY_BUFFER, gl.createBuffer());
  gl.bufferData(
    gl.ARRAY_BUFFER,
    new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]),
    gl.STATIC_DRAW
  );
  const position = gl.getAttribLocation(program, "a");
  gl.enableVertexAttribArray(position);
  gl.vertexAttribPointer(position, 2, gl.FLOAT, false, 0, 0);
  gl.enable(gl.BLEND);
  gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
  return { gl, uniform: (name) => gl.getUniformLocation(program, name) };
};

/**
 * A command's progress on the rim of a pill (DESIGN.md §8 "Progress rim"): two arcs of drifting
 * light grow from the bottom centre, up each side, as far as `stepRim` says, and meet at the top
 * when the run ends; then the rim fades and `onSettled` is called. A run that stops drains back
 * instead. It draws on a WebGL canvas outside React's renders, once a frame, only while a run is
 * shown; without WebGL it draws nothing but still keeps the run's time. Place it in the pill, a
 * positioned element it measures. `md` suits the floating bar; `sm`, a quieter glow, a small
 * control. The canvas carries the drawn share in `data-progress`.
 */
export const ProgressRim = ({
  run,
  onSettled,
  size = "md",
}: {
  run: RimRun | undefined;
  onSettled: () => void;
  size?: keyof typeof SIZES;
}) => {
  const { pad, scale, strength } = SIZES[size];
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const programRef = useRef<Program | null>(null);
  const sizeRef = useRef({ dpr: 1, h: 0, w: 0 });
  // Read once a frame, outside React's renders.
  const currentRun = useEffectEvent(() => run);
  const settled = useEffectEvent(() => {
    onSettled();
  });

  // The pill's size, border included; the canvas sits `pad` outside it.
  useLayoutEffect(() => {
    const canvas = canvasRef.current;
    const bar = canvas?.parentElement ?? null;
    const measure = (c: HTMLCanvasElement, b: HTMLElement) => {
      const w = b.offsetWidth;
      const h = b.offsetHeight;
      const dpr = Math.min(2, window.devicePixelRatio || 1);
      const border = b.clientLeft;
      c.style.left = `${-pad - border}px`;
      c.style.top = `${-pad - border}px`;
      c.style.width = `${w + 2 * pad}px`;
      c.style.height = `${h + 2 * pad}px`;
      c.width = Math.round((w + 2 * pad) * dpr);
      c.height = Math.round((h + 2 * pad) * dpr);
      sizeRef.current = { dpr, h, w };
    };
    const observer = new ResizeObserver(() => {
      if (canvas !== null && bar !== null) {
        measure(canvas, bar);
      }
    });
    if (canvas !== null && bar !== null) {
      measure(canvas, bar);
      observer.observe(bar);
    }
    return () => {
      observer.disconnect();
    };
  }, [pad]);

  const shown = run !== undefined;
  useEffect(() => {
    const canvas = canvasRef.current;
    let handle = 0;
    if (shown && canvas !== null) {
      const program = programRef.current ?? compile(canvas) ?? null;
      programRef.current = program;
      const light = tokenRgb(
        canvas,
        "--color-floating-bar-rim",
        [0.83, 1, 0.31]
      );
      const flow = tokenRgb(
        canvas,
        "--color-floating-bar-rim-flow",
        [0.65, 0.95, 0.91]
      );
      const still = window.matchMedia(
        "(prefers-reduced-motion: reduce)"
      ).matches;
      let frame: RimFrame = RIM_START;
      let lastState: RimRun["state"] = "running";
      let last = performance.now();
      const draw = (now: number) => {
        const current = currentRun();
        if (current === undefined) {
          return;
        }
        // A new run that starts while the last one's rim still fades starts over, from its light.
        if (current.state === "running" && lastState !== "running") {
          frame = { ...RIM_START, alpha: frame.alpha };
        }
        lastState = current.state;
        frame = stepRim(frame, current, Math.min(0.05, (now - last) / 1000));
        last = now;
        canvas.dataset.progress = frame.progress.toFixed(3);
        canvas.dataset.sweep = frame.sweep.toFixed(3);
        if (program !== null) {
          const { gl, uniform } = program;
          const { dpr, h, w } = sizeRef.current;
          gl.viewport(0, 0, canvas.width, canvas.height);
          gl.clearColor(0, 0, 0, 0);
          gl.clear(gl.COLOR_BUFFER_BIT);
          gl.uniform2f(uniform("uRes"), canvas.width, canvas.height);
          gl.uniform1f(uniform("uDpr"), dpr);
          // Reduced motion keeps the light still; the progress itself still grows.
          gl.uniform1f(uniform("uTime"), still ? 0 : frame.elapsed);
          gl.uniform1f(uniform("uProg"), frame.progress);
          gl.uniform1f(uniform("uAlpha"), frame.alpha);
          gl.uniform1f(uniform("uPad"), pad);
          gl.uniform1f(uniform("uSettle"), frame.settle);
          gl.uniform1f(uniform("uScale"), scale);
          gl.uniform1f(uniform("uStrength"), strength);
          gl.uniform1f(uniform("uSweep"), frame.sweep);
          gl.uniform1f(uniform("uSweepAt"), frame.sweepAt);
          // Reduced motion: the sweep brightens the whole rim instead of running.
          gl.uniform1f(uniform("uStill"), still ? 1 : 0);
          gl.uniform2f(uniform("uBar"), w, h);
          gl.uniform3f(uniform("uLight"), ...light);
          gl.uniform3f(uniform("uFlow"), ...flow);
          gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
        }
        if (frame.finished) {
          settled();
          return;
        }
        handle = requestAnimationFrame(draw);
      };
      handle = requestAnimationFrame(draw);
    }
    return () => {
      cancelAnimationFrame(handle);
      if (canvas !== null) {
        delete canvas.dataset.progress;
        delete canvas.dataset.sweep;
      }
      const gl = programRef.current?.gl;
      gl?.clear(gl.COLOR_BUFFER_BIT);
    };
  }, [shown, pad, scale, strength]);

  return (
    <canvas
      ref={canvasRef}
      aria-hidden
      className="pointer-events-none absolute"
    />
  );
};
