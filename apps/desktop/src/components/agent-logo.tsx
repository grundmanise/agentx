import { cva } from "class-variance-authority";

import claudeCode from "#/assets/logos/claude-code.svg";
import codex from "#/assets/logos/codex.svg";
import cursor from "#/assets/logos/cursor.svg";
import geminiCli from "#/assets/logos/gemini-cli.svg";
import githubCopilot from "#/assets/logos/github-copilot.svg";
import windsurf from "#/assets/logos/windsurf.svg";
import type { Configuration } from "#/cli/events.gen.ts";

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

/** The colour of the monogram's letter: `on-accent`, read from the tokens as the page has them. */
const letterColour = () =>
  getComputedStyle(document.documentElement)
    .getPropertyValue("--color-on-accent")
    .trim();

/** A monogram as an image: the first letter of the client's name, drawn in place. */
const monogram = (name: string) => {
  const letter = name.slice(0, 1).toUpperCase();
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><text x="12" y="17.5" text-anchor="middle" font-family="system-ui, sans-serif" font-size="15" font-weight="600" fill="${letterColour()}">${letter}</text></svg>`;
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`;
};

/** The sizes of DESIGN.md §6 "Agent client logo chip" this app uses so far, in pixels. */
export type LogoSize = 16 | 20 | 24 | 28;

// A round white tile, opaque whether or not the client turned the declaration off, so a
// neighbour's ring never shows through it; the ring (`logo.ring`) is the parent's background,
// between stacked logos.
const tileVariants = cva("rounded-logo bg-logo-bg inline-flex flex-none", {
  variants: {
    ring: {
      false: "",
      true: "ring-row-chip-ring ring-2",
    },
    size: {
      16: "p-logo-sm size-4",
      20: "p-logo-sm size-5",
      24: "size-6 p-1",
      28: "p-logo-lg size-7",
    },
  },
});

// The mark inside the tile: dimmed to half and drained of colour when the client turned it off.
const markVariants = cva("block size-full object-contain", {
  variants: {
    off: {
      false: "",
      true: "opacity-50 grayscale",
    },
  },
});

/** The configuration a logo stands for, and what the logo says about it. */
export interface LogoClient extends Pick<Configuration, "client" | "name"> {
  /** The client turned this off: the mark is dimmed on the white disc, and `title` says so. */
  off?: boolean;
  /** The tooltip, in place of the client's name. */
  title?: string;
}

export interface AgentLogoProps {
  /** The configuration's `client` slug, which picks the logo, and its `name`, for the title. */
  client: LogoClient;
  size: LogoSize;
  /** Draws the ring behind the logo, for logos that overlap. */
  ring?: boolean;
  /** Inside a control that names the client itself: the image then adds nothing to its name. */
  decorative?: boolean;
}

/**
 * A round agent client logo (DESIGN.md §6 "Agent client logo chip"): the bundled image, or a
 * monogram of the client's name when none is bundled. Every logo is an image named after the
 * client, with the name as its title; one the client turned off says so in its title and carries
 * `data-off`.
 */
export const AgentLogo = ({
  client,
  size,
  ring = false,
  decorative = false,
}: AgentLogoProps) => {
  const off = client.off === true;
  return (
    <span className={tileVariants({ ring, size })}>
      <img
        src={logos[client.client] ?? monogram(client.name)}
        alt={decorative ? "" : client.name}
        aria-hidden={decorative ? true : undefined}
        title={client.title ?? client.name}
        data-off={off ? "true" : undefined}
        width={size}
        height={size}
        draggable={false}
        className={markVariants({ off })}
      />
    </span>
  );
};

const stackVariants = cva("flex items-center", {
  variants: {
    size: {
      16: "[&>*+*]:-ml-1.5",
      20: "[&>*+*]:-ml-1.5",
      24: "[&>*+*]:-ml-2",
      28: "[&>*+*]:-ml-2",
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
  decorative = false,
}: {
  clients: LogoClient[];
  size: LogoSize;
  extra?: number;
  decorative?: boolean;
}) => (
  <span className={stackVariants({ size })}>
    {clients.map((c) => (
      <AgentLogo
        key={`${c.client} ${c.name}`}
        client={c}
        size={size}
        ring
        decorative={decorative}
      />
    ))}
    {extra > 0 && (
      <span className="type-technical-sm text-text-hint pl-2">{`+${extra}`}</span>
    )}
  </span>
);
