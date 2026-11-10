// Gives a test the app's own notify(), so it can raise a toast of any length. The dev server
// serves this import at the URL the app uses, so notify() reaches the app's own Toaster.
import { notify } from "#/components/toast.tsx";

Object.assign(window, { agentxNotify: notify });
