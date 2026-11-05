/**
 * The 40px tile of a plugin card (DESIGN.md §6 "Plugin card"): the plugin's own icon, which the
 * inventory carries as a data URL so nothing is fetched (ADR 0001), or a monogram of its name when
 * it carries none. The tile is never dimmed, for a disabled plugin included: its pill says so.
 */
export const PluginTile = ({ name, icon }: { name: string; icon?: string }) =>
  icon === undefined ? (
    <span
      aria-hidden
      className="bg-button-secondary-bg rounded-card-tile size-plugin-card-tile type-plugin-name text-text-primary flex flex-none items-center justify-center"
    >
      {name.slice(0, 1).toUpperCase()}
    </span>
  ) : (
    <img
      src={icon}
      alt={name}
      width={40}
      height={40}
      draggable={false}
      className="rounded-card-tile size-plugin-card-tile flex-none object-contain"
    />
  );
