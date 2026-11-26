// Opens the Run checks dialog, as Settings › Run checks does. The dev server serves this import at
// the URL the app uses, so it reaches the app's own startup state.
import { runChecks } from "#/startup/startup.ts";

runChecks();
