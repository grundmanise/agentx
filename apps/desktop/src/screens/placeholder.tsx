/**
 * A screen not built yet: its page title only. Each screen replaces this in its own change;
 * nothing here shows data, since the app renders only what the CLI reports.
 */
export const PlaceholderScreen = ({ title }: { title: string }) => (
  <div className="pt-page-top px-page pb-page h-full overflow-auto">
    <h1 className="type-page-title text-text-primary m-0">{title}</h1>
  </div>
);
