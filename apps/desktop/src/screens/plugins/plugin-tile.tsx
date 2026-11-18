import { cva } from "class-variance-authority";

const tileVariants = cva(
  "rounded-card-tile flex flex-none items-center justify-center",
  {
    variants: {
      kind: {
        icon: "object-contain",
        monogram: "bg-button-secondary-bg type-plugin-tile text-text-primary",
      },
      size: {
        detail: "size-plugin-tile",
        row: "size-plugin-row-tile",
      },
    },
  }
);

const pixels = { detail: 40, row: 28 } as const;

/**
 * A plugin's tile (DESIGN.md §6 "Plugins screen"): the plugin's own icon, which the inventory
 * carries as a data URL so nothing is fetched (ADR 0001), or a monogram of its name when it
 * carries none. 40px in the detail pane, 28px in a list row, where the row names the plugin and
 * the tile is decorative. The tile is never dimmed, for a disabled plugin included: its pill says
 * so.
 */
export const PluginTile = ({
  name,
  icon,
  size,
}: {
  name: string;
  icon?: string;
  size: "detail" | "row";
}) =>
  icon === undefined ? (
    <span aria-hidden className={tileVariants({ kind: "monogram", size })}>
      {name.slice(0, 1).toUpperCase()}
    </span>
  ) : (
    <img
      src={icon}
      alt={size === "row" ? "" : name}
      width={pixels[size]}
      height={pixels[size]}
      draggable={false}
      className={tileVariants({ kind: "icon", size })}
    />
  );
