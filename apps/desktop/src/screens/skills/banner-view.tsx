// The one banner of the detail pane (DESIGN.md §6 "Banner" and "Banner actions and dialogs"),
// with its button: Add to agents on the gray banner, Put back on the amber one. A Put back the
// CLI refuses because the directory holds another version opens a confirm dialog, whose Replace
// runs the same command with --force.
import { cva } from "class-variance-authority";
import { useState } from "react";

import { EXIT_CODES } from "#/cli/events.gen.ts";
import type { LibraryPlacement } from "#/cli/events.gen.ts";
import { AlertIcon } from "#/components/icons/alert.tsx";
import { Spinner } from "#/components/spinner.tsx";
import { dismissToast } from "#/components/toast.tsx";
import { Button } from "#/components/ui/button.tsx";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "#/components/ui/dialog.tsx";
import type { Banner } from "#/screens/skills/banners.ts";
import {
  agentsWord,
  holdsOtherContent,
  placedCount,
} from "#/screens/skills/placements.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";
import { runUntilSettled, useSettling } from "#/store/settle.ts";

const bannerVariants = cva(
  "rounded-banner py-banner pr-banner-right pl-banner gap-banner flex items-center border",
  {
    variants: {
      tone: {
        attention: "border-tone-attention-border bg-tone-attention-bg",
        danger: "border-tone-danger-border bg-tone-danger-bg",
        neutral: "border-tone-neutral-border bg-tone-neutral-bg",
        primary: "border-tone-primary-border bg-tone-primary-bg",
        yours: "border-tone-yours-border bg-tone-yours-bg",
      },
    },
  }
);

const tileVariants = cva(
  "rounded-card-tile flex size-8 flex-none items-center justify-center",
  {
    variants: {
      tone: {
        attention: "text-tone-attention-fg",
        danger: "text-tone-danger-fg",
        neutral: "text-tone-neutral-fg",
        primary: "text-tone-primary-fg",
        yours: "text-tone-yours-fg",
      },
    },
  }
);

/** A small ghost button that runs a command, pending while the command and its inventory come. */
const ActionButton = ({
  label,
  pending,
  onClick,
}: {
  label: string;
  pending: boolean;
  onClick: () => void;
}) => (
  <Button
    variant="ghost"
    size="sm"
    disabled={pending}
    aria-busy={pending ? true : undefined}
    onClick={onClick}
  >
    {pending && <Spinner />}
    {label}
  </Button>
);

/** Add to agents: `skill place <name>`, into every enabled agent. */
const AddToAgents = ({ row }: { row: SkillRow }) => {
  const label = `place ${row.key}`;
  const pending = useSettling(label);
  return (
    <ActionButton
      label="Add to agents"
      pending={pending}
      onClick={() => {
        void runUntilSettled(["skill", "place", row.name], {
          label,
          success: (events) =>
            `Added ${row.name} to ${agentsWord(placedCount(events))}`,
        });
      }}
    />
  );
};

/**
 * Put back: `skill place <name> --to <configuration>`. When the CLI refuses because the directory
 * holds another version, the dialog asks before running it again with --force.
 */
const PutBack = ({
  row,
  placement,
  agent,
}: {
  row: SkillRow;
  placement: LibraryPlacement;
  agent: string;
}) => {
  const label = `put back ${row.key} ${placement.configuration}`;
  const pending = useSettling(label);
  const [confirm, setConfirm] = useState(false);
  const putBack = async (force: boolean) => {
    const args = ["skill", "place", row.name, "--to", placement.configuration];
    const outcome = await runUntilSettled(force ? [...args, "--force"] : args, {
      label,
      success: `Put ${row.name} back in ${agent}`,
    });
    if (
      !force &&
      outcome.exitCode === EXIT_CODES.refused &&
      holdsOtherContent(outcome.events)
    ) {
      // The dialog answers this refusal: the error toast would only repeat it.
      dismissToast();
      setConfirm(true);
    }
  };
  return (
    <>
      <ActionButton
        label="Put back"
        pending={pending}
        onClick={() => {
          void putBack(false);
        }}
      />
      <Dialog open={confirm} onOpenChange={setConfirm}>
        <DialogContent>
          <DialogHeader showCloseButton={false}>
            <DialogTitle>{`Replace ${agent}'s folder?`}</DialogTitle>
            <DialogDescription>
              {`It holds a different version of ${row.name}. Putting the link back deletes what it holds.`}
            </DialogDescription>
          </DialogHeader>
          <p className="type-technical text-text-caption m-0 break-all">
            {placement.path}
          </p>
          <DialogFooter>
            <DialogClose asChild>
              <Button variant="ghost">Cancel</Button>
            </DialogClose>
            <Button
              variant="danger"
              onClick={() => {
                setConfirm(false);
                void putBack(true);
              }}
            >
              Replace
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};

const BannerActions = ({ row, banner }: { row: SkillRow; banner: Banner }) => {
  switch (banner.action?.kind) {
    case "add-to-agents": {
      return <AddToAgents row={row} />;
    }
    case "put-back": {
      return (
        <PutBack
          row={row}
          placement={banner.action.placement}
          agent={banner.action.agent}
        />
      );
    }
    case undefined: {
      return null;
    }
    default: {
      return null;
    }
  }
};

export const BannerView = ({
  row,
  banner,
}: {
  row: SkillRow;
  banner: Banner;
}) => (
  <div
    role="note"
    aria-label={banner.title}
    className={bannerVariants({ tone: banner.tone })}
  >
    <span className={tileVariants({ tone: banner.tone })}>
      <AlertIcon size="lg" />
    </span>
    <div className="gap-banner-text flex min-w-0 flex-1 flex-col">
      <span className="type-banner-title text-banner-title">
        {banner.title}
      </span>
      {banner.text !== undefined && (
        <span className="type-secondary text-banner-text">{banner.text}</span>
      )}
    </div>
    <div className="flex items-center gap-1.5">
      <BannerActions row={row} banner={banner} />
    </div>
  </div>
);
