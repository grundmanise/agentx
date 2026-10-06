import { NavLink } from "react-router";
import { AgentxMark } from "@/components/agentx-mark";
import { Icon } from "@/components/icon";
import { cn } from "@/lib/utils";
import { screens } from "./screens";

export function Sidebar() {
  return (
    <aside className="flex w-(--shell-sidebar-width) flex-none flex-col gap-(--shell-nav-gap) bg-shell-bg p-(--shell-sidebar-padding)">
      <div className="flex items-center gap-2.5 px-2.5 pt-1 pb-[22px]">
        <AgentxMark size={26} className="rounded-shell-logo" />
        {/* The machine label joins the brand once the app reads settings from the CLI. */}
        <span className="text-[14px] font-semibold tracking-[-0.01em]">agentx</span>
      </div>
      <nav aria-label="Main" className="flex flex-col gap-(--shell-nav-gap)">
        {screens.map((s) => (
          <NavLink
            key={s.path}
            to={s.path}
            className={({ isActive }) =>
              cn(
                "flex h-(--shell-nav-height) items-center gap-[11px] rounded-shell-nav px-2.5 text-[14px] font-medium",
                s.groupStart && "mt-[18px]",
                isActive
                  ? "bg-shell-nav-active-bg text-shell-nav-active-text"
                  : "text-shell-nav-text hover:text-text-primary",
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
}
