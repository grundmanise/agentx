// What vite.config.ts defines beside Vite's own variables.
interface ImportMetaEnv {
  /** Names this build of the app: a launch reads only what its own build kept. */
  readonly APP_BUILD: string;
}
