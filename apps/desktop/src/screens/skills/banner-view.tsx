// The one banner of the detail pane (DESIGN.md §6 "Banner" and "Banner actions and dialogs"),
// with its button: Update on the lime banners of an update, Add to agents on the gray "Not in any
// agent" banner, Reconnect on the amber one, Adopt, Add source or Fork on the others. The buttons
// are in src/screens/skills/actions.tsx.
import { cva } from "class-variance-authority";

import { AlertIcon } from "#/components/icons/alert.tsx";
import { RefreshIcon } from "#/components/icons/refresh.tsx";
import {
  AddSource,
  AddToAgents,
  Adopt,
  Fork,
  Reconnect,
  UpdateButton,
} from "#/screens/skills/actions.tsx";
import type { Banner } from "#/screens/skills/banners.ts";
import type { SkillRow } from "#/screens/skills/projection.ts";

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
      return <UpdateButton row={row} />;
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
      return <Fork name={row.name} />;
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
