// shadcn/ui Tabs (`pnpm dlx shadcn@latest add tabs`), on our tokens: DESIGN.md §6 "Tabs". It keeps
// shadcn's parts, their `data-slot` attributes and their default look (a filled track with the
// active tab raised on it), and takes no `className`. Only the horizontal default variant is kept:
// the app has no vertical or line tabs.
import { Tabs as TabsPrimitive } from "radix-ui";

import type { OwnProps } from "#/lib/utils.ts";

const Tabs = ({ ...props }: OwnProps<typeof TabsPrimitive.Root>) => (
  <TabsPrimitive.Root
    data-slot="tabs"
    className="gap-tabs flex flex-col"
    {...props}
  />
);

const TabsList = ({ ...props }: OwnProps<typeof TabsPrimitive.List>) => (
  <TabsPrimitive.List
    data-slot="tabs-list"
    className="bg-tabs-list-bg text-tabs-text rounded-tabs-list h-tabs-list p-tabs-list inline-flex w-fit items-center justify-center"
    {...props}
  />
);

const TabsTrigger = ({ ...props }: OwnProps<typeof TabsPrimitive.Trigger>) => (
  <TabsPrimitive.Trigger
    data-slot="tabs-trigger"
    className="rounded-tabs-trigger type-tab text-tabs-text hover:text-text-primary data-[state=active]:bg-tabs-active-bg data-[state=active]:border-tabs-active-border data-[state=active]:text-tabs-active-text focus-visible:outline-shell-focus-ring inline-flex h-full flex-1 items-center justify-center gap-1.5 border border-transparent px-2.5 py-1 whitespace-nowrap focus-visible:outline-2 disabled:pointer-events-none disabled:opacity-50"
    {...props}
  />
);

const TabsContent = ({ ...props }: OwnProps<typeof TabsPrimitive.Content>) => (
  <TabsPrimitive.Content
    data-slot="tabs-content"
    className="flex-1 outline-none"
    {...props}
  />
);

export { Tabs, TabsContent, TabsList, TabsTrigger };
