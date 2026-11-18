// The one banner of the detail pane (DESIGN.md §6 "Banner" and "Banner actions and dialogs"),
// with its button: See changes and Update on the lime banners of an update (See changes opens the
// What's new sheet), Show folder, Abort and Update on the amber one of a pending merge, Add to
// agents on the gray "Not in any agent" banner, Reconnect on the amber one of a displaced skill,
// Adopt or Add source on the others, Publish or Settings on your own skills' banners, and
// Publish to on an edited skill of a source this machine can write to. The banner of a skill gone
// from its source has no button: Fork is in the action row. The buttons are in
// src/screens/skills/actions.tsx, but Publish and Settings (src/screens/skills/publish-dialog.tsx).
// The box itself is a Callout (src/components/callout.tsx).
import { Callout } from "#/components/callout.tsx";
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
import type { SkillRow } from "#/screens/skills/projection.ts";
import {
  OpenSettingsButton,
  PublishButton,
} from "#/screens/skills/publish-dialog.tsx";

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
      return <Adopt row={row} />;
    }
    case "add-source": {
      return <AddSource source={banner.action.source} />;
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
  <Callout
    tone={banner.tone}
    icon={banner.icon === "update" ? "update" : "alert"}
    title={banner.title}
    text={banner.text}
    actions={<BannerActions row={row} banner={banner} />}
  />
);
