// Two toasts in a row, the second with an action that sets the page title. The dev server
// serves this import at the URL the app uses, so notify() reaches the app's own Toaster.
import { notify } from "#/components/toast.tsx";

notify("Added pdf to Cursor");
notify("Removed pdf from Cursor", {
  label: "Undo",
  onClick: () => {
    document.title = "undone";
  },
});
