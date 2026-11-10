// The one banner of the detail pane (DESIGN.md §6 "Banner" and "Banner actions and dialogs"),
// with its button: See changes and Update on the lime banners of an update (See changes opens the
// What's new sheet), Show folder, Abort and Update on the amber one of a pending merge, Add to
// agents on the gray "Not in any agent" banner, Reconnect on the amber one of a displaced skill,
// Adopt, Add source or Fork on the others, Publish or Settings on your own skills' banners, and
// Publish to on an edited skill of a source this machine can write to. The buttons are in
// src/screens/skills/actions.tsx, but Fork, which opens the Fork dialog
// (src/screens/skills/fork-dialog.tsx), and Publish and Settings (src/screens/skills/publish-dialog.tsx).
import { cva } from "class-variance-authority";

import { AlertIcon } from "#/components/icons/alert.tsx";
import { RefreshIcon } from "#/components/icons/refresh.tsx";
import {
  AbortMerge,
  AddSource,
  AddToAgents,
  Adopt,
  Reconnect,
  SeeChanges,
  ShowMergeFolder,
  UpdateButton,
} from "#/screens/skills/actions.tsx";
import type { Banner } from "#/screens/skills/banners.ts";
import { ForkButton } from "#/screens/skills/fork-dialog.tsx";
import type { SkillRow } from "#/screens/skills/projection.ts";
import {
  OpenSettingsButton,
  PublishButton,
} from "#/screens/skills/publish-dialog.tsx";

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

const BannerActions = ({ row, banner }: { row: SkillRow; banner: Banner }) => {
  switch (banner.action?.kind) {
    case "update": {
      return (
        <>
          <SeeChanges row={row} />
          <UpdateButton row={row} />
        </>
      );
    }
    case "conflict": {
      return (
        <>
          <ShowMergeFolder name={row.name} />
          <AbortMerge name={row.name} />
          <UpdateButton row={row} />
        </>
      );
    }
    case "add-to-agents": {
      return <AddToAgents row={row} />;
    }
    case "put-back": {
      return (
        <Reconnect
          row={row}
          placement={banner.action.placement}
          agent={banner.action.agent}
        />
      );
    }
    case "adopt": {
      return <Adopt name={row.name} />;
    }
    case "add-source": {
      return <AddSource source={banner.action.source} />;
    }
    case "fork": {
      return <ForkButton row={row} place="banner" />;
    }
    case "publish": {
      return <PublishButton row={row} label="Publish" />;
    }
    case "open-settings": {
      return <OpenSettingsButton />;
    }
    case "edited": {
      return (
        <PublishButton
          row={row}
          label={`Publish to ${banner.action.publishTo}`}
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
      {banner.icon === "update" ? (
        <RefreshIcon size="lg" />
      ) : (
        <AlertIcon size="lg" />
      )}
    </span>
    <div className="gap-banner-text flex min-w-0 flex-1 flex-col">
      <span className="type-banner-title text-banner-title">
        {banner.title}
      </span>
      {banner.text !== undefined && (
        <span className="type-secondary text-banner-text text-pretty">
          {banner.text}
        </span>
      )}
    </div>
    <div className="flex items-center gap-1.5">
      <BannerActions row={row} banner={banner} />
    </div>
  </div>
);
