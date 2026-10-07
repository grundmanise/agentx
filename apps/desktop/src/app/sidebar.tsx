import { NavLink } from "react-router";

import { screens } from "#/app/screens.ts";
import { AgentxMark } from "#/components/agentx-mark.tsx";
import { Icon } from "#/components/icon.tsx";
import { cn } from "#/lib/utils.ts";

export const Sidebar = () => (
  <aside className="bg-shell-bg flex w-(--shell-sidebar-width) flex-none flex-col gap-(--shell-nav-gap) p-(--shell-sidebar-padding)">
    <div className="flex items-center gap-2.5 px-2.5 pt-1 pb-5.5">
      <AgentxMark size={26} />
      {/* The machine label joins the brand once the app reads settings from the CLI. */}
      <span className="type-brand">agentx</span>
    </div>
    <nav aria-label="Main" className="flex flex-col gap-(--shell-nav-gap)">
      {screens.map((s) => (
        <NavLink
          key={s.path}
          to={s.path}
          className={({ isActive }) =>
            cn(
              "rounded-shell-nav type-nav flex h-(--shell-nav-height) items-center gap-(--shell-nav-icon-gap) px-2.5",
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
