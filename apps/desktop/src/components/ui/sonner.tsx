// shadcn/ui's Sonner toaster (https://ui.shadcn.com/docs/components/sonner), adapted to the
// app: dark only, our icons, and the colours from tokens. Its colours are Sonner's own CSS
// variables, which `src/styles/index.css` sets from the tokens, as shadcn sets them with `style`.
// A toast's action is the app's own `Button`, which `src/components/toast.tsx` gives Sonner; the
// close button stays Sonner's, named Dismiss.
import { Toaster as Sonner } from "sonner";

import { AlertTriangleIcon } from "#/components/icons/alert-triangle.tsx";
import { CircleCheckIcon } from "#/components/icons/circle-check.tsx";
import { CircleXIcon } from "#/components/icons/circle-x.tsx";
import { Spinner } from "#/components/spinner.tsx";

/**
 * Where toasts show (DESIGN.md §6 "Toast"): top right, `toast.offset` from the window's edges,
 * one at a time, tinted by outcome with Sonner's rich colours.
 */
export const Toaster = () => (
  <Sonner
    theme="dark"
    position="top-right"
    // toast.offset
    offset={24}
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
