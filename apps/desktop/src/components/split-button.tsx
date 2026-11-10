// A split button (DESIGN.md §6 "Split button"): an action and, joined to it, a chevron that opens
// a menu of related actions. Show in Finder on a file row and Add in the Skills header are two.
import type { ReactNode } from "react";

import { ChevronIcon } from "#/components/icons/chevron.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "#/components/ui/dropdown-menu.tsx";

/**
 * Two ghost buttons that meet with no gap: the action, then the chevron, which opens `menu`
 * (`DropdownMenuItem`s). `size` is `icon` for an action that shows an icon, named by `label`, and
 * `sm` for one that shows its label.
 */
export const SplitButton = ({
  size,
  children,
  label,
  title,
  onClick,
  menuLabel,
  menuTitle,
  onOpenChange,
  menu,
}: {
  size: "icon" | "sm";
  /** What the action shows: its icon, or its label. */
  children: ReactNode;
  /** The action's accessible name, for an action that shows an icon. */
  label?: string;
  title?: string;
  onClick: () => void;
  /** The chevron's accessible name. */
  menuLabel: string;
  menuTitle?: string;
  onOpenChange?: (open: boolean) => void;
  menu: ReactNode;
}) => (
  <span className="inline-flex">
    <Button
      variant="ghost"
      size={size === "icon" ? "icon-split-main" : "sm-split-main"}
      aria-label={label}
      title={title}
      onClick={onClick}
    >
      {children}
    </Button>
    <DropdownMenu onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size={size === "icon" ? "icon-split-menu" : "sm-split-menu"}
          aria-label={menuLabel}
          title={menuTitle}
        >
          <span className="flex rotate-90">
            <ChevronIcon size="xs" />
          </span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>{menu}</DropdownMenuContent>
    </DropdownMenu>
  </span>
);
