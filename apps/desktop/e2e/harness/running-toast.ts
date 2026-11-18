// A running command's toast, with its Cancel, then its outcome. The dev server serves this import
// at the URL the app uses, so the toasts reach the app's own Toaster.
import { notify, notifySuccess } from "#/components/toast.tsx";

const cancel = {
  label: "Cancel",
  onClick: () => {
    document.title = "cancelled";
  },
};
notify("Checking for updates…", cancel, { kind: "progress", lasting: true });
notify("Checking for updates… 1/2", cancel, {
  kind: "progress",
  lasting: true,
});
window.showOutcome = () => {
  notifySuccess("All skills are up to date");
};
