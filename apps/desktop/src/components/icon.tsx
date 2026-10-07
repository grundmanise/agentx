// Stroke icons on a 24 grid (DESIGN.md §7). Paths come from the design mock.
export const icons = {
  inbox:
    "M22 12h-6l-2 3h-4l-2-3H2M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z",
  skills: "M12 3 3 8l9 5 9-5-9-5zM3 16l9 5 9-5M3 12l9 5 9-5",
  add: "M12 8v8M8 12h8M12 3a9 9 0 1 0 0 18 9 9 0 1 0 0-18z",
  agents: "M12 8V4H8M4 8h16v12H4zM2 14h2M20 14h2M9 13v2M15 13v2",
  servers: "M4 4h16v6H4zM4 14h16v6H4zM8 7h.01M8 17h.01",
  plugins: "M9 3v4M15 3v4M6 7h12v4a6 6 0 0 1-12 0zM12 17v4",
  settings: "M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M14 4v4M8 10v4M16 16v4",
  search: "M11 4a7 7 0 1 0 0 14 7 7 0 1 0 0-14zM20 20l-3.5-3.5",
} as const;

export type IconName = keyof typeof icons;

/** Rendered sizes and stroke widths DESIGN.md §7 allows. */
export type IconSize = 13 | 14 | 15 | 16 | 18;
export type IconStroke = 1.75 | 2 | 2.25;

/**
 * The icon's colour. `current` takes the text colour around it. `palette` is the command palette's
 * icon grey. `palette-row` is that grey, turning to the accent while its row (a `group`) is selected.
 */
const tones = {
  current: undefined,
  palette: "text-palette-icon",
  "palette-row": "text-palette-icon group-data-[selected=true]:text-palette-icon-active",
} as const;

export type IconTone = keyof typeof tones;

export function Icon({
  name,
  size = 16,
  strokeWidth = 1.75,
  tone = "current",
}: {
  name: IconName;
  size?: IconSize;
  strokeWidth?: IconStroke;
  tone?: IconTone;
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={strokeWidth}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden
      className={tones[tone]}
    >
      <path d={icons[name]} />
    </svg>
  );
}
