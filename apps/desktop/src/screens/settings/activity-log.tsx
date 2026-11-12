// Activity log: opens the log of the last commands the app ran (src/app/activity-log.tsx).
import { Button } from "#/components/ui/button.tsx";
import { SettingsSection } from "#/screens/settings/section.tsx";
import { openActivityLog } from "#/store/activity-log.ts";

export const ActivityLogSection = () => (
  <SettingsSection
    title="Activity log"
    description="The last commands the app ran, with the failures in red"
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
