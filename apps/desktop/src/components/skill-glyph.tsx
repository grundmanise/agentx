import { cva } from "class-variance-authority";

/** The three glyphs of DESIGN.md §6 "Skill glyph". */
export type GlyphShape = "dot" | "pencil" | "fork";

const glyphVariants = cva("block flex-none", {
  variants: {
    size: {
      12: "size-3",
      16: "size-4",
      18: "size-4.5",
    },
    tone: {
      caption: "text-text-caption",
      edited: "text-glyph-edited",
      inherit: "",
      lit: "text-glyph-enabled",
      off: "text-glyph-off",
    },
  },
});

/** How the glyph is coloured: by the skill's state, or as its parent says for a marker. */
export type GlyphTone = "state" | "caption" | "inherit";

const toneOf = (shape: GlyphShape, lit: boolean, tone: GlyphTone) => {
  if (tone !== "state") {
    return tone;
  }
  if (shape === "pencil") {
    return "edited";
  }
  return lit ? "lit" : "off";
};

/**
 * The glyph leading a skill row and the detail header: a filled dot for a skill of a source or an
 * unmanaged one, a pencil when it was edited, a fork for one of your own. `lit` is `glyph.enabled`;
 * the pencil is always `glyph.edited`. The title says what the glyph means, as a tooltip and to a
 * screen reader. At 12px it is a marker beside a row's name or in a pill: the fork glyph on the
 * original of a fork made under a new name, in `caption` or in its parent's colour.
 */
export const SkillGlyph = ({
  shape,
  lit,
  title,
  size = 16,
  tone = "state",
  decorative = false,
}: {
  shape: GlyphShape;
  lit: boolean;
  title: string;
  size?: 12 | 16 | 18;
  tone?: GlyphTone;
  /** Inside a control that names itself: the glyph then adds nothing to its name. */
  decorative?: boolean;
}) => (
  <span title={title} className="flex">
    <svg
      viewBox="0 0 16 16"
      aria-hidden
      focusable="false"
      className={glyphVariants({ size, tone: toneOf(shape, lit, tone) })}
    >
      {shape === "dot" && <circle cx="8" cy="8" r="4" fill="currentColor" />}
      {shape === "pencil" && (
        <path
          d="M3 13l.8-3.2L10.6 3a1.3 1.3 0 0 1 1.8 0l.6.6a1.3 1.3 0 0 1 0 1.8L6.2 12.2 3 13zm6.4-8.6 1.2 1.2"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      )}
      {shape === "fork" && (
        <g
          fill="none"
          stroke="currentColor"
          strokeWidth="1.5"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <circle cx="4.5" cy="3.5" r="1.6" />
          <circle cx="11.5" cy="3.5" r="1.6" />
          <circle cx="8" cy="12.5" r="1.6" />
          <path d="M4.5 5.1v1.4a2 2 0 0 0 2 2h3a2 2 0 0 0 2-2V5.1M8 8.5v2.4" />
        </g>
      )}
    </svg>
    {!decorative && <span className="sr-only">{title}</span>}
  </span>
);
