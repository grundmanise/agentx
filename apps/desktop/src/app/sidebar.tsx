import { NavLink } from "react-router";

import { screens } from "#/app/screens.ts";
import { AgentxMark } from "#/components/agentx-mark.tsx";
import { Icon } from "#/components/icon.tsx";
import { cn } from "#/lib/utils.ts";

export const Sidebar = () => (
  <aside className="bg-shell-bg w-sidebar gap-nav pt-sidebar-top pr-sidebar-right pb-sidebar pl-sidebar flex flex-none flex-col">
    <div className="flex items-center gap-2.5 px-2.5 pt-1 pb-5.5">
      <AgentxMark size={26} />
      {/* The machine label joins the brand once the app reads settings from the CLI. */}
      <span className="type-brand">agentx</span>
    </div>
    <nav aria-label="Main" className="gap-nav flex flex-col">
      {screens.map((s) => (
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
          <Icon name={s.icon} />
          <span className="flex-1">{s.label}</span>
        </NavLink>
      ))}
    </nav>
  </aside>
);
