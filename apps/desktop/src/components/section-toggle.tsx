import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

import { ChevronIcon } from "#/components/icons/chevron.tsx";
import { CollapsibleTrigger } from "#/components/ui/collapsible.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "#/components/ui/tooltip.tsx";

const toggleVariants = cva(
  "group/toggle rounded-row gap-group focus-visible:outline-shell-focus-ring flex min-w-0 items-center text-left focus-visible:outline-2",
  {
    variants: {
      // With an action, the row that holds the toggle and the action tints instead.
      action: {
        false: "hover:bg-subgroup-hover w-full",
        true: "flex-1",
      },
      level: {
        group: "pt-group-top px-group pb-group-bottom min-h-group-header",
        subgroup: "px-subgroup-x py-subgroup-y",
      },
    },
  }
);

const rowVariants = cva(
  "rounded-row hover:bg-subgroup-hover flex w-full items-center",
  {
    variants: {
      level: {
        group: "pr-group",
        subgroup: "pr-subgroup-x",
      },
    },
  }
);

const labelVariants = cva("type-caption min-w-0 truncate", {
  variants: {
    level: { group: "text-group-label", subgroup: "text-subgroup-label" },
  },
});

const countVariants = cva("type-technical-sm", {
  variants: {
    level: { group: "text-group-count", subgroup: "text-subgroup-count" },
  },
});

/**
 * The header of a section that collapses, within a list (DESIGN.md §6 "Group header (lists)"):
 * a flat, full-width row with no fill at rest, a chevron that turns down while the section is
 * open, the section's caption, its count and, at the right end, a dim detail such as a plugin's
 * marketplace. It tints on hover. It is the `CollapsibleTrigger` of the shadcn `Collapsible`
 * that holds it, which holds the open state: a click collapses or expands the section, which
 * `aria-expanded` reports, and the chevron follows the trigger's `data-state`. It is named by its
 * caption, count and detail. An action, such as a text button, sits at the row's right end,
 * beside the toggle, so that a click on it toggles nothing. A `group` header heads a top-level
 * group of a list, with the group's padding and colours; a `subgroup` header heads a section
 * inside one, such as a plugin's skills.
 */
export const SectionToggle = ({
  label,
  icon,
  title,
  count,
  detail,
  action,
  level = "subgroup",
}: {
  /** The section's caption, uppercase like every caption. */
  label: string;
  /** A small mark before the caption, such as an agent's logo. */
  icon?: ReactNode;
  /** The caption's tooltip, such as a source's full URL. */
  title?: string;
  /** How many rows the section holds; left out, the header shows no count. */
  count?: number;
  /**
   * A dim note at the row's right end, such as a plugin's marketplace. It takes only the space
   * the caption and count leave, so it truncates first and disappears when none is left; its
   * tooltip shows it in full.
   */
  detail?: string;
  /** A control at the row's right end, outside the toggle. */
  action?: ReactNode;
  /** A top-level group's header, or a section's inside one. */
  level?: "group" | "subgroup";
}) => {
  const hasAction = action !== undefined && action !== null;
  const caption = <span className={labelVariants({ level })}>{label}</span>;
  const toggle = (
    <CollapsibleTrigger asChild>
      <button
        type="button"
        className={toggleVariants({
          action: hasAction,
          level,
        })}
      >
        <span className="text-text-caption flex group-data-[state=open]/toggle:rotate-90">
          <ChevronIcon size="xs" />
        </span>
        {icon !== undefined && <span className="flex flex-none">{icon}</span>}
        {title === undefined ? (
          caption
        ) : (
          <Tooltip>
            <TooltipTrigger asChild>{caption}</TooltipTrigger>
            <TooltipContent side="top" align="start">
              {title}
            </TooltipContent>
          </Tooltip>
        )}
        {count !== undefined && (
          <span className={countVariants({ level })}>{count}</span>
        )}
        {detail !== undefined && (
          <span
            title={detail}
            className="type-technical-sm text-subgroup-source min-w-0 flex-1 truncate text-right"
          >
            {detail}
          </span>
        )}
      </button>
    </CollapsibleTrigger>
  );
  if (!hasAction) {
    return toggle;
  }
  return (
    <div className={rowVariants({ level })}>
      {toggle}
      <span className="flex">{action}</span>
    </div>
  );
};
