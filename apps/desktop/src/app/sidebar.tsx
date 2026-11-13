import { NavLink, useLocation } from "react-router";

import { screens } from "#/app/screens.ts";
import type { Screen } from "#/app/screens.ts";
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

const noScreens: Screen[] = [];

/**
 * One nav item. A screen with nested screens is active on its own routes but not on theirs, so
 * Skills is active on a skill and on Update review, and Sources alone on its page.
 */
const NavItem = ({
  screen: { icon: ScreenIcon, label, path, children = noScreens },
  nested = false,
}: {
  screen: Screen;
  nested?: boolean;
}) => {
  const { pathname } = useLocation();
  const onChild = children.some(
    (c) => pathname === c.path || pathname.startsWith(`${c.path}/`)
  );
  return (
    <NavLink
      to={path}
      className={({ isActive }) =>
        cn(
          "rounded-shell-nav type-nav gap-nav-icon flex items-center px-2.5",
          nested ? "h-8" : "h-nav",
          isActive && !onChild
            ? "bg-shell-nav-active-bg text-shell-nav-active-text"
            : "text-shell-nav-text hover:text-text-primary"
        )
      }
      // NavLink marks every ancestor of the route as current; a parent of the open screen is not.
      aria-current={onChild ? false : undefined}
    >
      {/* The icon takes the link's text colour, so it brightens with the label. */}
      <ScreenIcon />
      <span className="flex-1">{label}</span>
    </NavLink>
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
      {screens.map((s) => (
        <div key={s.path} className="gap-nav flex flex-col">
          <NavItem screen={s} />
          {s.children !== undefined && (
            // Nested screens hang off a `shell.nav-tree` rule under their parent's icon.
            <div className="gap-nav border-shell-nav-tree ml-4.5 flex flex-col border-l pl-2">
              {s.children.map((child) => (
                <NavItem key={child.path} screen={child} nested />
              ))}
            </div>
          )}
        </div>
      ))}
    </nav>
  </aside>
);
