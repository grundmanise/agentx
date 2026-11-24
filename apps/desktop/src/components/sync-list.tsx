// The parts of the sync popover that the title bar's status opens (DESIGN.md §6 "Publish status
// (title bar)"): a group of skills under its caption, a row for each skill, a short line, and the
// footer that names the account repo and when it was last checked. Each is a closed part: it
// takes what it shows and what to call, never a class.
import { useId } from "react";
import type { ReactNode } from "react";

/** A group of rows under its caption, named by it: To publish, Incoming. */
export const SyncGroup = ({
  caption,
  children,
}: {
  caption: string;
  children: ReactNode;
}) => {
  const id = useId();
  return (
    <section aria-labelledby={id} className="flex flex-col gap-1">
      <h3 id={id} className="type-caption text-popover-caption m-0 mb-1 px-2">
        {caption}
      </h3>
      {children}
    </section>
  );
};

/** The rows of a group, one skill each. */
export const SyncRows = ({ children }: { children: ReactNode }) => (
  <ul className="m-0 flex list-none flex-col p-0">{children}</ul>
);

const row =
  "rounded-popover-row flex h-8 w-full items-center justify-between gap-2.5 px-2 text-left";

/**
 * A skill in a group: its name, and what the group says of it at the right. With `onOpen` the
 * row is a button that opens the skill.
 */
export const SyncRow = ({
  name,
  meta,
  onOpen,
}: {
  name: string;
  meta?: string;
  onOpen?: () => void;
}) => {
  const content = (
    <>
      <span className="type-secondary text-text-primary truncate">{name}</span>
      {meta !== undefined && (
        <span className="type-secondary text-text-muted flex-none">{meta}</span>
      )}
    </>
  );
  return (
    <li>
      {onOpen === undefined ? (
        <div className={row}>{content}</div>
      ) : (
        <button
          type="button"
          onClick={onOpen}
          className="rounded-popover-row hover:bg-popover-row-hover focus-visible:outline-shell-focus-ring flex h-8 w-full cursor-pointer items-center justify-between gap-2.5 px-2 text-left focus-visible:outline-2 focus-visible:outline-offset-2"
        >
          {content}
        </button>
      )}
    </li>
  );
};

/** One short line in the popover, such as what the synced state says. */
export const SyncNote = ({ children }: { children: ReactNode }) => (
  <p className="type-secondary text-text-muted m-0 px-2">{children}</p>
);

/**
 * The account repo's short form at the left, when it was last checked at the right. The remote
 * never truncates: the popover widens to fit it, and in a window too narrow even for that, the
 * time wraps under it.
 */
export const SyncFooter = ({
  remote,
  checked,
}: {
  remote: string;
  checked: string;
}) => (
  <div className="flex flex-wrap items-center justify-between gap-x-2.5 gap-y-1 px-2">
    <span className="type-technical-sm text-text-caption break-all">
      {remote}
    </span>
    <span className="type-secondary text-text-hint flex-none">{checked}</span>
  </div>
);
