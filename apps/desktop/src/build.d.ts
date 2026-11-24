// What vite.config.ts defines beside Vite's own variables, and the build's environment values.
interface ImportMetaEnv {
  /** Names this build of the app: a launch reads only what its own build kept. */
  readonly APP_BUILD: string;
  /** Where usage data goes (src/lib/telemetry.ts); set by a release build alone. */
  readonly VITE_TELEMETRY_HOST?: string;
  /** The project key usage data is sent with; set by a release build alone. */
  readonly VITE_TELEMETRY_KEY?: string;
}
