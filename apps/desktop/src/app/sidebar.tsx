import { Link, useLocation } from "react-router";

import { screenOf, useLastRoute } from "#/app/kept-screens.tsx";
import { screens } from "#/app/screens.ts";
import type { Screen } from "#/app/screens.ts";
import { AgentxMark } from "#/components/agentx-mark.tsx";
import { ArrowUpIcon } from "#/components/icons/arrow-up.tsx";
import { PillButton } from "#/components/pill.tsx";
import { plural } from "#/lib/plural.ts";
import { cn } from "#/lib/utils.ts";
import { UpdateSkillsDialog } from "#/screens/skills/update-dialog.tsx";
import {
  UPDATE_PILL_ID,
  openUpdateDialog,
  useOfferedUpdates,
} from "#/screens/skills/updates.ts";
import { useAppStore } from "#/store/store.ts";

/** This machine's label, from the inventory, under the product name. */
const MachineLabel = () => {
  const label = useAppStore((s) => s.inventory?.machine.label);
  return label === undefined ? null : (
    <span className="type-machine-label text-text-muted truncate">{label}</span>
  );
};

/**
 * One nav item, active on its own routes. A nested screen has a path of its own, outside its
 * parent's, so Skills is active on a skill and on Update review, and Sources alone on its page.
 * It goes back to the route its screen last showed, such as the skill selected there.
 */
const NavItem = ({
  screen: { icon: ScreenIcon, label, path },
  nested = false,
}: {
  screen: Screen;
  nested?: boolean;
}) => {
  const active = screenOf(useLocation().pathname)?.path === path;
  return (
    <Link
      to={useLastRoute(path)}
      aria-current={active ? "page" : undefined}
      className={cn(
        "rounded-shell-nav type-nav gap-nav-icon flex items-center px-2.5",
        nested ? "h-8" : "h-nav",
        active
          ? "bg-shell-nav-active-bg text-shell-nav-active-text"
          : "text-shell-nav-text hover:text-text-primary"
      )}
    >
      {/* The icon takes the link's text colour, so it brightens with the label. */}
      <ScreenIcon />
      <span className="flex-1">{label}</span>
    </Link>
  );
};

/**
 * The update pill at the right of the Skills nav item, while the last update check found an
 * update the app offers: "1 update", "4 updates". It sits beside the link, not in it, so a click
 * opens the Update skills dialog and goes nowhere.
 */
const UpdatePill = () => {
  const n = useOfferedUpdates().length;
  return n === 0 ? null : (
    <span className="absolute top-1/2 right-2 flex -translate-y-1/2">
      <PillButton
        id={UPDATE_PILL_ID}
        tone="update"
        icon={<ArrowUpIcon size="2xs" />}
        label={plural(n, "update")}
        title="Update skills"
        onClick={openUpdateDialog}
      >
        {plural(n, "update")}
      </PillButton>
    </span>
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
          {s.path === "/skills" ? (
            <div className="relative flex flex-col">
              <NavItem screen={s} />
              <UpdatePill />
            </div>
          ) : (
            <NavItem screen={s} />
          )}
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
    <UpdateSkillsDialog />
  </aside>
);
