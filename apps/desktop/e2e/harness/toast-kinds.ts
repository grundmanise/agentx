// A success toast, and `window.showFailure()`, which replaces it with a failure in the app's own
// words. The dev server serves this import at the URL the app uses, so the toasts reach the app's
// own Toaster.
import { notifyFailure, notifySuccess } from "#/components/toast.tsx";

notifySuccess("Added 2 skills");
window.showFailure = () => {
  notifyFailure("Couldn't open the file dialog");
};
