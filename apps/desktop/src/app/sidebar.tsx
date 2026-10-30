import { NavLink } from "react-router";

import { screens } from "#/app/screens.ts";
import { AgentxMark } from "#/components/agentx-mark.tsx";
import { cn } from "#/lib/utils.ts";
import { useAppStore } from "#/store/store.ts";

/** This machine's label, from the inventory, under the product name. */
const MachineLabel = () => {
  const label = useAppStore((s) => s.inventory?.machine.label);
  return label === undefined ? null : (
    <span className="type-machine-label text-text-muted truncate">{label}</span>
  );
};

export const Sidebar = () => (
  <aside className="bg-shell-bg w-sidebar gap-nav pt-sidebar-top pr-sidebar-right pb-sidebar pl-sidebar flex flex-none flex-col">
    <div className="flex items-center gap-2.5 px-2.5 pt-1 pb-5.5">
      <AgentxMark size={30} />
      <div className="flex min-w-0 flex-col">
        <span className="type-brand">agentx</span>
        <MachineLabel />
      </div>
    </div>
    <nav aria-label="Main" className="gap-nav flex flex-col">
      {screens.map(({ icon: ScreenIcon, ...s }) => (
        <NavLink
          key={s.path}
          to={s.path}
          className={({ isActive }) =>
            cn(
              "rounded-shell-nav type-nav h-nav gap-nav-icon flex items-center px-2.5",
              isActive
                ? "bg-shell-nav-active-bg text-shell-nav-active-text"
                : "text-shell-nav-text hover:text-text-primary"
            )
          }
        >
          {/* The icon takes the link's text colour, so it brightens with the label. */}
          <ScreenIcon />
          <span className="flex-1">{s.label}</span>
        </NavLink>
      ))}
    </nav>
  </aside>
);
