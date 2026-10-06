/**
 * A screen not built yet: its page title only. Each screen replaces this in its own change;
 * nothing here shows data, since the app renders only what the CLI reports.
 */
export function PlaceholderScreen({ title }: { title: string }) {
  return (
    <div className="h-full overflow-auto p-(--page-padding)">
      <h1 className="m-0 type-page-title text-text-primary">{title}</h1>
    </div>
  );
}
