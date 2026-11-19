/**
 * The WebGL program of a progress rim (DESIGN.md §8 "Progress rim"): two arcs of drifting light
 * that grow from the bottom centre of a pill, up each side, and meet at the top. Every length is
 * in CSS pixels; the canvas is `uPad` larger than the pill on each side, for the glow. `uScale`
 * sizes the glow and the head, `uStrength` how bright the glow is. At the end of a finished run a
 * bright band (`uSweep`, at `uSweepAt`) runs from the top centre back down both sides; with
 * `uStill` (reduced motion) it brightens the whole rim instead. `uFullLine`, `uFullGlow` and
 * `uFullWidth` say how bright and how wide the joined, full ring is beside the growing arcs, so
 * that on a small pill the band stands out from the ring under it.
 */

export const RIM_VERTEX = `
attribute vec2 a;
void main() { gl_Position = vec4(a, 0.0, 1.0); }
`;

export const RIM_FRAGMENT = `
precision highp float;
uniform vec2 uRes;
uniform float uDpr, uTime, uProg, uAlpha, uPad, uSettle, uScale, uStrength, uSweep, uSweepAt, uStill, uFullLine, uFullGlow, uFullWidth;
uniform vec2 uBar;
uniform vec3 uLight, uFlow;

float hash(vec2 p) { return fract(sin(dot(p, vec2(127.1, 311.7))) * 43758.5453); }
float noise(vec2 p) {
  vec2 i = floor(p), f = fract(p);
  vec2 u = f * f * (3.0 - 2.0 * f);
  return mix(mix(hash(i), hash(i + vec2(1, 0)), u.x), mix(hash(i + vec2(0, 1)), hash(i + vec2(1, 1)), u.x), u.y);
}
float fbm(vec2 p) {
  float v = 0.0, a = 0.5;
  for (int i = 0; i < 4; i++) { v += a * noise(p); p *= 2.03; a *= 0.5; }
  return v;
}

void main() {
  float PI = 3.14159265;
  vec2 c = gl_FragCoord.xy / uDpr;
  // Relative to the bar's centre, y down.
  vec2 p = vec2(c.x - uPad - uBar.x * 0.5, (uRes.y / uDpr - c.y) - uPad - uBar.y * 0.5);
  float r = uBar.y * 0.5, L = uBar.x - uBar.y;
  // Signed distance to the border, which sits 0.75px inside the bar's edge.
  vec2 q = vec2(max(abs(p.x) - L * 0.5, 0.0), p.y);
  float d = length(q) - r + 0.75;
  // Where along the border, from the top centre clockwise.
  float P = 2.0 * L + 2.0 * PI * r, s;
  if (abs(p.x) <= L * 0.5) {
    if (p.y < 0.0) s = p.x >= 0.0 ? p.x : P + p.x;
    else s = L * 0.5 + PI * r + (L * 0.5 - p.x);
  } else if (p.x > 0.0) {
    s = L * 0.5 + (atan(p.y, p.x - L * 0.5) + PI * 0.5) * r;
  } else {
    float a = atan(p.y, p.x + L * 0.5);
    if (a < 0.0) a += 2.0 * PI;
    s = L * 0.5 + PI * r + L + (a - PI * 0.5) * r;
  }
  // Measured from the bottom centre, up either side: 0 there, P/2 at the top centre.
  float S = abs(s - (L + PI * r));
  float full = uSettle;
  float headS = uProg * P * 0.5;
  // A long soft leading edge, and light that builds toward the head, evening out near the end.
  float lead = mix(1.0 - smoothstep(headS - 42.0 * uScale, headS + 2.0, S), 1.0, full);
  float along = clamp(S / max(headS, 1.0), 0.0, 1.0);
  float rampMin = mix(0.4, 1.0, smoothstep(0.8, 1.0, uProg));
  float ramp = mix(mix(rampMin, 1.0, pow(along, 1.5)), 1.0, full);
  float mask = lead * ramp;
  // Noise from the 2D position, drifting slowly.
  float n = fbm(vec2(p.x * 0.011 - uTime * 0.32, p.y * 0.045 + uTime * 0.1));
  float core = exp(-d * d / 0.65);
  float glow = d > 0.0 ? exp(-d / ((7.0 + 3.0 * n) * uScale)) : exp(d / 1.5);
  // The joined ring: calmer than the growing arcs where the size asks for it. The band keeps its own light.
  float width = mix(1.0, uFullWidth, full);
  float ringCore = exp(-d * d / (0.65 * width * width)) * mix(1.0, uFullLine, full);
  float ringGlow = (d > 0.0 ? exp(-d / ((7.0 + 3.0 * n) * uScale * width)) : exp(d / (1.5 * width))) * mix(1.0, uFullGlow, full);
  float dh = headS - S;
  float hw = dh >= 0.0 ? 30.0 * uScale : 5.0;
  float gw = (dh >= 0.0 ? 30.0 : 14.0) * uScale;
  float head = (1.0 - full) * exp(-dh * dh / (2.0 * hw * hw));
  float headGlow = (1.0 - full) * exp(-dh * dh / (2.0 * gw * gw));
  vec3 pale = mix(uLight, vec3(1.0), 0.6);
  vec3 col = mix(uLight, uFlow, smoothstep(0.3, 0.85, n) * 0.55);
  col = mix(col, pale, head * 0.45);
  float breathe = 0.9 + 0.1 * sin(uTime * 1.5);
  float a = mask * (ringCore + ringGlow * 0.4 * uStrength * breathe * (0.75 + 0.5 * n)) + head * core * 0.35 + headGlow * glow * 0.3 * uStrength;
  // The sweep: a bright band from the top centre back down both sides.
  float Sb = (1.0 - uSweepAt) * P * 0.5;
  float bw = 26.0 * uScale + 6.0;
  float band = uSweep * (uStill > 0.5 ? 1.0 : exp(-(S - Sb) * (S - Sb) / (2.0 * bw * bw)));
  a += band * (core * 0.9 + glow * 0.75 * uStrength);
  col = mix(col, pale, band * 0.75);
  // Fades out before the canvas's edge, so the glow has no hard border.
  a *= 1.0 - smoothstep(uPad * 0.45, uPad - 1.0, d);
  a += core * 0.045;
  a = clamp(a, 0.0, 1.0) * uAlpha;
  gl_FragColor = vec4(col * a, a);
}
`;
