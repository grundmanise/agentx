// Activity log: opens the log of every command the app ran this session (src/app/activity-log.tsx).
import { Button } from "#/components/ui/button.tsx";
import { SettingsSection } from "#/screens/settings/section.tsx";
import { openActivityLog } from "#/store/activity-log.ts";

export const ActivityLogSection = () => (
  <SettingsSection
    title="Activity log"
    description="Every command the app ran since it opened, and how each ended"
  >
    <div className="flex">
      <Button
        variant="secondary"
        onClick={() => {
          openActivityLog();
        }}
      >
        Show activity log
      </Button>
    </div>
  </SettingsSection>
);
