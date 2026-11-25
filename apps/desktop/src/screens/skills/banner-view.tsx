// The one banner of the detail pane (DESIGN.md §6 "Banner" and "Banner actions and dialogs"),
// with its button: See changes and Update on the lime banners of an update (See changes opens the
// What's new sheet), Show folder, Discard my edits and Apply merge on the amber one of a pending merge, Add to
// agents on the gray "Not in any agent" banner, Reconnect on the amber one of a displaced skill,
// Adopt, Open or Add source on the others, Publish or Settings on hub skills' banners, and
// Publish to on an edited skill of a source this machine can write to. The banner of a skill gone
// from its source has no button: Fork is in the action row. The buttons are in
// src/screens/skills/actions.tsx, but Publish and Settings (src/screens/skills/publish-dialog.tsx).
// The box itself is an Alert (src/components/ui/alert.tsx).
import {
  Alert,
  AlertAction,
  AlertContent,
  AlertDescription,
  AlertTile,
  AlertTitle,
} from "#/components/ui/alert.tsx";
import {
  AddSource,
  AddToAgents,
  Adopt,
  ApplyMerge,
  DiscardEdits,
  OpenSkill,
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
          <DiscardEdits row={row} />
          <ApplyMerge row={row} />
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
    case "open-skill": {
      return <OpenSkill name={banner.action.name} />;
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
      return <PublishButton row={row} label="Publish" />;
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
  <Alert tone={banner.tone} aria-label={banner.title}>
    <AlertTile tone={banner.tone} icon={banner.icon} />
    <AlertContent>
      <AlertTitle>{banner.title}</AlertTitle>
      {banner.text !== undefined && (
        <AlertDescription>{banner.text}</AlertDescription>
      )}
    </AlertContent>
    <AlertAction>
      <BannerActions row={row} banner={banner} />
    </AlertAction>
  </Alert>
);
