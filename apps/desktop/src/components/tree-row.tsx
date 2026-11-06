import { cva } from "class-variance-authority";

import { ChevronIcon } from "#/components/icons/chevron.tsx";

/** The tone of a tree row's dot: the group it is in (DESIGN.md §6 "Plugin card"). */
export type TreeTone = "skills" | "servers" | "hooks";

const rowVariants = cva(
  "h-plugin-card-row relative flex w-full items-center gap-2.5 pr-2.5 pl-5.5 text-left",
  {
    variants: {
      clickable: {
        false: "",
        true: "rounded-row hover:bg-plugin-row-hover",
      },
    },
  }
);

// The connector: a vertical line through the row, which stops at the middle on the last row, and
// a stub from it to the dot.
const lineVariants = cva(
  "bg-plugin-connector pointer-events-none absolute top-0 left-2.5 w-px",
  {
    variants: {
      last: {
        false: "bottom-0",
        true: "bottom-1/2",
      },
    },
  }
);

const dotVariants = cva("rounded-pill size-1.5 flex-none", {
  variants: {
    tone: {
      hooks: "bg-plugin-hooks-dot",
      servers: "bg-plugin-servers-dot",
      skills: "bg-plugin-skills-dot",
    },
  },
});

export interface TreeRowProps {
  tone: TreeTone;
  /** The row's name, and its accessible name when it opens something. */
  name: string;
  /** Mono meta after the name: a server's transport, a hook's event. */
  meta?: string;
  /** The last row of its group: its connector line stops at the middle. */
  last: boolean;
  /** Opens what the row names; a row without one is display only, with no chevron. */
  onOpen?: () => void;
}

const Content = ({
  tone,
  name,
  meta,
  last,
  chevron,
}: TreeRowProps & { chevron: boolean }) => (
  <>
    <span aria-hidden className={lineVariants({ last })} />
    <span
      aria-hidden
      className="bg-plugin-connector pointer-events-none absolute top-1/2 left-2.5 h-px w-2.5"
    />
    <span aria-hidden className={dotVariants({ tone })} />
    <span className="type-row-name text-text-primary min-w-0 flex-1 truncate">
      {name}
    </span>
    {meta !== undefined && meta !== "" && (
      <span className="type-technical-sm text-text-caption whitespace-nowrap">
        {meta}
      </span>
    )}
    {chevron && (
      <span className="text-text-caption flex">
        <ChevronIcon size="xs" />
      </span>
    )}
  </>
);

/**
 * One row of a nested-content tree (DESIGN.md §6 "Plugin card"): a connector line, the group's
 * dot, the name, mono meta and, when the row opens something, a chevron. With `onOpen` it is a
 * button named by `name`; without, plain text.
 */
export const TreeRow = (props: TreeRowProps) =>
  props.onOpen === undefined ? (
    <span className={rowVariants({ clickable: false })}>
      <Content {...props} chevron={false} />
    </span>
  ) : (
    <button
      type="button"
      aria-label={props.name}
      onClick={props.onOpen}
      className={rowVariants({ clickable: true })}
    >
      <Content {...props} chevron />
    </button>
  );
