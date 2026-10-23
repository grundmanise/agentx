// Opens the startup checks in manual mode, as Settings › Run checks will. The dev server serves
// this import at the URL the app uses, so it reaches the app's own startup state.
import { runChecks } from "#/startup/startup.ts";

runChecks();
