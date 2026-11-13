import { cva } from "class-variance-authority";
import type { HTMLAttributes, RefCallback } from "react";

import claudeCode from "#/assets/logos/claude-code.svg";
import codex from "#/assets/logos/codex.svg";
import cursor from "#/assets/logos/cursor.svg";
import geminiCli from "#/assets/logos/gemini-cli.svg";
import githubCopilot from "#/assets/logos/github-copilot.svg";
import windsurf from "#/assets/logos/windsurf.svg";
import type { Configuration } from "#/cli/events.gen.ts";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";

/**
 * The logos bundled with the app, by client slug. The app never loads an image from the network
 * (ADR 0001): a client with no logo here gets a monogram.
 */
const logos: Record<string, string> = {
  "claude-code": claudeCode,
  codex,
  cursor,
  "gemini-cli": geminiCli,
  "github-copilot": githubCopilot,
  windsurf,
};

/**
 * The two letters of a monogram: the initials of the first two words of the name ("Kilo Code" is
 * "KC", "roo-code" is "RC"), or, for a one-word name, its first letter and the next ("Aider" is
 * "Ai").
 */
const monogramLetters = (name: string) => {
  const words = name.split(/[\s_-]+/u).filter((w) => w !== "");
  const [first = "", second] = words;
  if (second !== undefined) {
    return `${first.charAt(0)}${second.charAt(0)}`.toUpperCase();
  }
  return `${first.charAt(0).toUpperCase()}${first.charAt(1).toLowerCase()}`;
};

/** How many monogram hues `tokens.css` holds, `monogram.0` to `monogram.9`. */
const HUES = 10;

/** The monogram hue of a name: the same name always gets the same one. */
const hueOf = (name: string) => {
  let hash = 0;
  for (const char of name) {
    hash = (hash * 31 + (char.codePointAt(0) ?? 0)) % 2_147_483_647;
  }
  return hash % HUES;
};

/** Text safe inside the monogram's SVG. */
const escapeXml = (text: string) =>
  text.replaceAll("&", "&amp;").replaceAll("<", "&lt;").replaceAll(">", "&gt;");

/** A colour token as the page has it, such as `--color-monogram-3-bg`. */
const token = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim();

/**
 * The monograms made so far, by name. Reading the colours forces the page to compute its styles,
 * so they are read once per name, not on every render of every logo; the tokens have one theme.
 */
const monograms = new Map<string, string>();

/**
 * A monogram as an image: two letters of the client's name in `monogram.N.text` on a round
 * `monogram.N.bg` tint, N picked by the name's hash.
 */
const monogram = (name: string) => {
  const made = monograms.get(name);
  if (made !== undefined) {
    return made;
  }
  const hue = hueOf(name);
  const bg = token(`--color-monogram-${hue}-bg`);
  const text = token(`--color-monogram-${hue}-text`);
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><circle cx="12" cy="12" r="12" fill="${bg}"/><text x="12" y="15.5" text-anchor="middle" font-family="system-ui, sans-serif" font-size="10" font-weight="600" fill="${text}">${escapeXml(monogramLetters(name))}</text></svg>`;
  const url = `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
  monograms.set(name, url);
  return url;
};

/** The sizes of DESIGN.md §6 "Agent client logo chip" this app uses so far, in pixels. */
export type LogoSize = 16 | 20 | 24 | 28 | 44;

/** The surface stacked logos sit on, which colours the ring between them (`logo.ring`). */
export type LogoSurface = "content" | "card";

// A round box of the logo's size. A bundled logo is a white tile, padded by size, the image
// contained; a monogram fills the box, its tint drawn in its own image. The ring
// (`logo.ring`) is the parent's background, between stacked logos. A logo is never dimmed or
// greyed.
const tileVariants = cva("rounded-logo block flex-none", {
  compoundVariants: [
    { className: "ring-row-chip-ring", ring: true, surface: "content" },
    { className: "ring-card-chip-ring", ring: true, surface: "card" },
    { className: "p-logo-sm", monogram: false, size: [16, 20] },
    { className: "p-1", monogram: false, size: 24 },
    { className: "p-logo-lg", monogram: false, size: 28 },
    { className: "p-logo-xl", monogram: false, size: 44 },
  ],
  defaultVariants: { monogram: false, surface: "content" },
  variants: {
    monogram: {
      false: "bg-logo-bg object-contain",
      true: "",
    },
    ring: {
      false: "",
      true: "ring-2",
    },
    size: {
      16: "size-4",
      20: "size-5",
      24: "size-6",
      28: "size-7",
      44: "size-agent-card-logo",
    },
    surface: {
      card: "",
      content: "",
    },
  },
});

/**
 * How a logo names its client to the pointer: a native `title`, the app's Tooltip, or nothing,
 * for a logo inside a control that shows the name itself or carries the tooltip.
 */
export type LogoHover = "title" | "tooltip" | "none";

export interface AgentLogoProps {
  /** The configuration's `client` slug, which picks the logo, and its `name`, for the title. */
  client: Pick<Configuration, "client" | "name">;
  size: LogoSize;
  /** Draws the ring behind the logo, for logos that overlap. */
  ring?: boolean;
  /** What the ringed logo sits on: the ring takes that colour. */
  surface?: LogoSurface;
  /** Inside a control that names the client itself: the image then adds nothing to its name. */
  decorative?: boolean;
  /** How the logo names its client on hover; a `title` unless the caller says otherwise. */
  hover?: LogoHover;
}

/**
 * What a Tooltip's trigger adds to the logo it wraps: its handlers, state and ref, passed on to
 * the image.
 */
type TriggerProps = Omit<
  HTMLAttributes<HTMLImageElement>,
  "children" | "className" | "dangerouslySetInnerHTML" | "style"
> & {
  ref?: RefCallback<HTMLImageElement>;
};

/** The logo itself: the bundled image, or the monogram drawn as an image. */
const LogoImage = ({
  client,
  size,
  ring = false,
  surface,
  decorative = false,
  hover = "title",
  ...trigger
}: AgentLogoProps & TriggerProps) => {
  const bundled = logos[client.client];
  return (
    <img
      {...trigger}
      src={bundled ?? monogram(client.name)}
      alt={decorative ? "" : client.name}
      aria-hidden={decorative ? true : undefined}
      title={hover === "title" ? client.name : undefined}
      width={size}
      height={size}
      draggable={false}
      className={tileVariants({
        monogram: bundled === undefined,
        ring,
        size,
        surface,
      })}
    />
  );
};

/**
 * A round agent client logo (DESIGN.md §6 "Agent client logo chip"): the bundled image, or a
 * monogram of the client's name when none is bundled. Every logo is an image named after the
 * client, with the name on hover: as its title, or in a Tooltip with `hover="tooltip"`.
 */
export const AgentLogo = ({ hover = "title", ...props }: AgentLogoProps) =>
  hover === "tooltip" ? (
    <Tooltip>
      <TooltipTrigger asChild>
        <LogoImage hover="none" {...props} />
      </TooltipTrigger>
      <TooltipContent>{props.client.name}</TooltipContent>
    </Tooltip>
  ) : (
    <LogoImage hover={hover} {...props} />
  );

const stackVariants = cva("flex items-center", {
  variants: {
    size: {
      16: "[&>*+*]:-ml-1.5",
      20: "[&>*+*]:-ml-1.5",
      24: "[&>*+*]:-ml-2",
      28: "[&>*+*]:-ml-2",
      44: "[&>*+*]:-ml-2",
    },
  },
});

/**
 * Logos stacked with `logo.overlap`, each ringed in the parent's background, the first on top of
 * the next. `extra` adds a "+n" count after the last logo (`text.hint`).
 */
export const LogoStack = ({
  clients,
  size,
  extra = 0,
  surface,
  decorative = false,
  hover,
}: {
  clients: Pick<Configuration, "client" | "name">[];
  size: LogoSize;
  extra?: number;
  surface?: LogoSurface;
  decorative?: boolean;
  hover?: LogoHover;
}) => (
  <span className={stackVariants({ size })}>
    {clients.map((c) => (
      <AgentLogo
        key={`${c.client} ${c.name}`}
        client={c}
        size={size}
        ring
        surface={surface}
        decorative={decorative}
        hover={hover}
      />
    ))}
    {extra > 0 && (
      <span className="type-technical-sm text-text-hint pl-2">{`+${extra}`}</span>
    )}
  </span>
);
