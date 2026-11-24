// shadcn/ui's Sonner toaster (https://ui.shadcn.com/docs/components/sonner), adapted to the
// app: dark only, our icons, and the colours from tokens. Its colours are Sonner's own CSS
// variables, which `src/styles/index.css` sets from the tokens, as shadcn sets them with `style`.
// A toast's action is the app's own `Button`, which `src/components/toast.tsx` gives Sonner; the
// close button stays Sonner's, named Dismiss.
import { Toaster as Sonner } from "sonner";

import { AlertTriangleIcon } from "#/components/icons/alert-triangle.tsx";
import { CircleCheckIcon } from "#/components/icons/circle-check.tsx";
import { CircleXIcon } from "#/components/icons/circle-x.tsx";
import { Spinner } from "#/components/ui/spinner.tsx";

/**
 * Where toasts show (DESIGN.md §6 "Toast"): top right, `toast.offset` below the title bar and
 * from the window's right edge, so a toast never covers the title bar's sync status. One at a
 * time, tinted by outcome with Sonner's rich colours. Both lengths are the tokens' own CSS
 * variables, so the toast follows the title bar's height.
 */
export const Toaster = () => (
  <Sonner
    theme="dark"
    position="top-right"
    offset={{
      right: "var(--inset-toast)",
      top: "calc(var(--height-titlebar) + var(--inset-toast))",
    }}
    visibleToasts={1}
    richColors
    icons={{
      error: <CircleXIcon />,
      loading: <Spinner />,
      success: <CircleCheckIcon />,
      warning: <AlertTriangleIcon />,
    }}
    toastOptions={{ closeButtonAriaLabel: "Dismiss" }}
  />
);
