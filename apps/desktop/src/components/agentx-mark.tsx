import { cva } from "class-variance-authority";

const X =
  "M227 0Q213 0 213 -10Q213 -20 226 -23L241 -26Q271 -33 257 -59L186 -199Q182 -206 176.5 -206.5Q171 -207 167 -199L102 -77Q80 -35 109 -29L135 -23Q148 -20 148 -10Q148 0 134 0H6Q-8 0 -8 -11Q-8 -22 4 -25L16 -28Q35 -33 48.0 -43.5Q61 -54 81 -91L155 -227Q163 -242 157 -256L62 -443Q51 -466 43.5 -474.0Q36 -482 26 -484L11 -487Q-2 -490 -2 -500Q-2 -510 12 -510H160Q174 -510 174 -500Q174 -490 161 -487L146 -484Q116 -477 130 -451L198 -319Q202 -312 207.0 -312.0Q212 -312 216 -319L276 -433Q298 -475 269 -481L243 -487Q230 -490 230 -500Q230 -510 244 -510H372Q386 -510 386 -499Q386 -488 374 -485L362 -482Q343 -478 330.0 -467.5Q317 -457 297 -419L228 -291Q219 -276 227 -260L325 -67Q337 -43 344.0 -35.5Q351 -28 361 -26L376 -23Q389 -20 389 -10Q389 0 375 0Z";

// Each size has its own corner radius: `shell.logo-radius` in the sidebar, `startup.logo-radius` on startup.
const markVariants = cva("ring-shell-logo-ring block flex-none ring-1", {
  variants: {
    size: {
      26: "rounded-shell-logo",
      40: "rounded-startup-logo",
    },
  },
});

/** The product mark: the X on its tile with a hairline ring (DESIGN.md §7): 26px in the sidebar, 40px on startup. */
export const AgentxMark = ({ size }: { size: 26 | 40 }) => (
  <svg
    width={size}
    height={size}
    viewBox="-220.8 -666.3 822.6 822.6"
    aria-label="agentx"
    // An inline SVG needs role="img" to be announced with its label.
    // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role
    role="img"
    className={markVariants({ size })}
  >
    <rect
      x="-220.8"
      y="-666.3"
      width="822.6"
      height="822.6"
      className="fill-shell-logo-bg"
    />
    <path d={X} className="fill-shell-logo-mark" />
  </svg>
);
