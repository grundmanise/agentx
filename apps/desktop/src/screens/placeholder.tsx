/**
 * A screen not built yet: its page title only. Each screen replaces this in its own change;
 * nothing here shows data, since the app renders only what the CLI reports.
 */
export function PlaceholderScreen({ title }: { title: string }) {
  return (
    <div className="h-full overflow-auto px-12 pt-10 pb-12">
      <h1 className="m-0 font-serif text-[56px] leading-none font-normal tracking-[-0.02em] text-text-primary">
        {title}
      </h1>
    </div>
  );
}
