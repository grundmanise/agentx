import { cva } from "class-variance-authority";

import mark from "#/assets/agentx-mark.svg";

// Each size has its own corner radius: `shell.logo-radius` in the sidebar, `startup.logo-radius` on startup.
const markVariants = cva("ring-shell-logo-ring block flex-none ring-1", {
  variants: {
    size: {
      26: "rounded-shell-logo",
      40: "rounded-startup-logo",
    },
  },
});

/**
 * The product mark: the X on its tile with a hairline ring (DESIGN.md §7): 26px in the sidebar,
 * 40px on startup. The image is `src/assets/agentx-mark.svg`, which is also the source of the app
 * icons.
 */
export const AgentxMark = ({ size }: { size: 26 | 40 }) => (
  <img
    src={mark}
    width={size}
    height={size}
    alt="agentx"
    className={markVariants({ size })}
  />
);
