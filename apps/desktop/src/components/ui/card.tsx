// shadcn/ui Card (`pnpm dlx shadcn@latest add card`), on our tokens: DESIGN.md §6 "Settings". It
// keeps shadcn's parts and their `data-slot` attributes and takes no `className`. Only the parts
// Settings uses: the card, its title and its content. The card is a `section`, so with
// `aria-labelledby` on its title it is a region.
import type { OwnProps } from "#/lib/utils.ts";

const Card = (props: OwnProps<"section">) => (
  <section
    data-slot="card"
    className="bg-card-bg border-card-border rounded-card pt-settings-card-top px-settings-card-x pb-settings-card-bottom flex flex-col border"
    {...props}
  />
);

const CardTitle = ({ children, ...props }: OwnProps<"h2">) => (
  <h2
    data-slot="card-title"
    className="type-settings-label text-text-primary m-0"
    {...props}
  >
    {children}
  </h2>
);

/** The card's rows, a 1px `card.border` hairline between two of them. */
const CardContent = (props: OwnProps<"div">) => (
  <div
    data-slot="card-content"
    className="divide-card-border flex flex-col divide-y"
    {...props}
  />
);

export { Card, CardContent, CardTitle };
