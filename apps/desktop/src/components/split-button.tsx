// An action with a menu of related actions (DESIGN.md §6 "Button group"), as shadcn's Button Group
// documents it: the action's button, the group's separator, then a three-dots icon button that
// opens the menu. Show in Finder on a file row and Add in the Skills header are two.
import type { ReactNode } from "react";

import { MoreIcon } from "#/components/icons/more.tsx";
import {
  ButtonGroup,
  ButtonGroupSeparator,
} from "#/components/ui/button-group.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "#/components/ui/dropdown-menu.tsx";

/**
 * The action, then the menu's trigger, in one ghost button group. `size` is `icon` for an action
 * that shows an icon, named by `label`, and `sm` for one that shows its label; the trigger is
 * square, as tall as the action.
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
  /** The trigger's accessible name. */
  menuLabel: string;
  menuTitle?: string;
  onOpenChange?: (open: boolean) => void;
  /** The menu's `DropdownMenuItem`s. */
  menu: ReactNode;
}) => (
  <ButtonGroup>
    <Button
      variant="ghost"
      size={size}
      aria-label={label}
      title={title}
      onClick={onClick}
    >
      {children}
    </Button>
    <ButtonGroupSeparator />
    <DropdownMenu onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          size={size === "icon" ? "icon" : "icon-sm"}
          aria-label={menuLabel}
          title={menuTitle}
        >
          <MoreIcon />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent>{menu}</DropdownMenuContent>
    </DropdownMenu>
  </ButtonGroup>
);
