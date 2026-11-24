// shadcn/ui Collapsible (`pnpm dlx shadcn@latest add collapsible`): a section that a trigger opens
// and closes. It keeps shadcn's parts and `data-slot` attributes and takes no `className`; like
// shadcn's, it draws nothing of its own, so each part is placed with `asChild` on the element that
// carries the look: the trigger on a `Button` or on the section toggle (`section-toggle.tsx`), the
// content on the list it shows. The trigger reports the state in `aria-expanded` and points at the
// content with `aria-controls`; a closed content is not in the document. No animation (DESIGN.md
// §8): the chevron turns with no transition.
import { Collapsible as CollapsiblePrimitive } from "radix-ui";

import type { OwnProps } from "#/lib/utils.ts";

const Collapsible = ({
  ...props
}: OwnProps<typeof CollapsiblePrimitive.Root>) => (
  <CollapsiblePrimitive.Root data-slot="collapsible" {...props} />
);

const CollapsibleTrigger = ({
  ...props
}: OwnProps<typeof CollapsiblePrimitive.CollapsibleTrigger>) => (
  <CollapsiblePrimitive.CollapsibleTrigger
    data-slot="collapsible-trigger"
    {...props}
  />
);

const CollapsibleContent = ({
  ...props
}: OwnProps<typeof CollapsiblePrimitive.CollapsibleContent>) => (
  <CollapsiblePrimitive.CollapsibleContent
    data-slot="collapsible-content"
    {...props}
  />
);

export { Collapsible, CollapsibleContent, CollapsibleTrigger };
