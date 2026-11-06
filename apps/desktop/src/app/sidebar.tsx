import { NavLink } from "react-router";

import { screens } from "#/app/screens.ts";
import { AgentxMark } from "#/components/agentx-mark.tsx";
import type { IconComponent } from "#/components/icons/icon-props.ts";
import { cn } from "#/lib/utils.ts";
import { useInboxCards } from "#/screens/inbox/cards.ts";
import { useAppStore } from "#/store/store.ts";

/** This machine's label, from the inventory, under the product name. */
const MachineLabel = () => {
  const label = useAppStore((s) => s.inventory?.machine.label);
  return label === undefined ? null : (
    <span className="type-machine-label text-text-muted truncate">{label}</span>
  );
};

/** How many cards the Inbox holds, on its nav item (`shell.nav-badge-bg`); nothing when none. */
const InboxBadge = ({ count }: { count: number }) =>
  count === 0 ? null : (
    <span className="rounded-pill bg-shell-nav-badge-bg text-on-accent type-nav-badge h-nav-badge min-w-nav-badge inline-flex items-center justify-center px-1.5">
      {count}
    </span>
  );

const NavItem = ({
  path,
  label,
  icon: ScreenIcon,
  inboxCount,
}: {
  path: string;
  label: string;
  icon: IconComponent;
  /** How many cards the Inbox holds; the Inbox item shows it and says it in its name. */
  inboxCount: number;
}) => {
  const inbox = path === "/inbox";
  return (
    <NavLink
      to={path}
      aria-label={
        inbox && inboxCount > 0 ? `${label}, ${inboxCount} need you` : undefined
      }
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
      <span className="flex-1">{label}</span>
      {inbox && <InboxBadge count={inboxCount} />}
    </NavLink>
  );
};

export const Sidebar = () => {
  // Projected once for the whole sidebar: the Inbox item alone shows it.
  const inboxCount = useInboxCards().length;
  return (
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
          <NavItem
            key={s.path}
            path={s.path}
            label={s.label}
            icon={s.icon}
            inboxCount={inboxCount}
          />
        ))}
      </nav>
    </aside>
  );
};
