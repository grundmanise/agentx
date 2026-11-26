// shadcn/ui Sheet (`pnpm dlx shadcn@latest add sheet`), on our tokens: DESIGN.md §6 "Sheet".
// The design has one sheet, at the right edge, so there is no `side`: the content keeps the right
// side's classes, which slide it in and out. Its parts keep shadcn's names and `data-slot`
// attributes and take no `className`; the close is our ghost Button, in the header. Every sheet has
// a handle on its left edge that drags its width, which each sheet keeps.
import { cva } from "class-variance-authority";
import { Dialog as SheetPrimitive } from "radix-ui";
import { useLayoutEffect, useRef } from "react";
import type { KeyboardEvent, PointerEvent, ReactNode } from "react";

import { CloseIcon } from "#/components/icons/close.tsx";
import { Button } from "#/components/ui/button.tsx";
import { cn } from "#/lib/utils.ts";
import type { OwnProps } from "#/lib/utils.ts";
import { keepSheetWidth, sheetWidth } from "#/store/sheet.ts";
import type { SheetName } from "#/store/sheet.ts";

const Sheet = ({ ...props }: OwnProps<typeof SheetPrimitive.Root>) => (
  <SheetPrimitive.Root data-slot="sheet" {...props} />
);

const SheetTrigger = ({
  ...props
}: OwnProps<typeof SheetPrimitive.Trigger>) => (
  <SheetPrimitive.Trigger data-slot="sheet-trigger" {...props} />
);

const SheetClose = ({ ...props }: OwnProps<typeof SheetPrimitive.Close>) => (
  <SheetPrimitive.Close data-slot="sheet-close" {...props} />
);

const SheetPortal = ({ ...props }: OwnProps<typeof SheetPrimitive.Portal>) => (
  <SheetPrimitive.Portal data-slot="sheet-portal" {...props} />
);

/**
 * What a sheet opens over: the page, under any dialog, or a dialog, such as the What's new sheet
 * of the Update skills dialog. Over a dialog, the sheet and its scrim take the dialog's layer,
 * later in the document, so they sit above it.
 */
type SheetLayer = "page" | "dialog";

const layerVariants = cva("", {
  defaultVariants: { over: "page" },
  variants: {
    over: {
      dialog: "z-50",
      page: "z-40",
    },
  },
});

/** `shell.scrim` behind the sheet, which fades in and out with it. */
const SheetOverlay = ({
  over,
  ...props
}: OwnProps<typeof SheetPrimitive.Overlay> & { over?: SheetLayer }) => (
  <SheetPrimitive.Overlay
    data-slot="sheet-overlay"
    className={cn(
      "bg-shell-scrim data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 fixed inset-0 data-[state=closed]:pointer-events-none!",
      layerVariants({ over })
    )}
    {...props}
  />
);

/**
 * `sheet.width`, which the user drags between `sheet.min-width` and `sheet.max-width`, inset
 * `sheet.inset` from the top, right and bottom, `sheet.bg` with `sheet.border`, `sheet.radius`,
 * `overlay.shadow` and `sheet.padding`. It slides in from past the window's right edge, and back
 * out, with shadcn's own classes (DESIGN.md §8), and takes no clicks while it closes.
 */
const SHEET_CONTENT =
  "rounded-sheet border-sheet-border bg-sheet-bg shadow-overlay top-sheet right-sheet bottom-sheet py-sheet-y px-sheet w-sheet-dragged min-w-sheet max-w-sheet data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:slide-out-to-right-sheet data-[state=open]:slide-in-from-right-sheet fixed flex flex-col gap-6 overflow-hidden border transition ease-in-out outline-none data-[state=closed]:pointer-events-none! data-[state=closed]:duration-200 data-[state=open]:duration-300";

/** The CSS variable that `w-sheet-dragged` reads, set on the sheet. */
const DRAGGED_WIDTH = "--sheet-dragged-width";

/** The width a sheet takes as drawn, which its minimum and maximum width have clamped. */
const drawnWidth = (sheet: HTMLElement) =>
  Math.round(sheet.getBoundingClientRect().width);

/**
 * Sets the width the sheet asks for, or its default when `width` is null. A negative width is
 * invalid CSS, which would drop the minimum width too, so it stops at 0 and the minimum clamps it.
 */
const setWidth = (sheet: HTMLElement, width: number | null) => {
  if (width === null) {
    sheet.style.removeProperty(DRAGGED_WIDTH);
  } else {
    sheet.style.setProperty(DRAGGED_WIDTH, `${Math.max(0, width)}px`);
  }
};

/**
 * The handle on the left edge of the sheet `name`, `sheet.handle-width` wide: dragging it moves
 * the sheet's left edge, and a double click or Home brings back the default width. It shows
 * `sheet.handle` on hover, and the focus ring colour on focus. The sheet opens at the width the
 * user left it at, which the handle keeps, clamped, when a drag ends.
 */
const ResizeHandle = ({ name }: { name: SheetName }) => {
  const handle = useRef<HTMLDivElement>(null);
  // Where the drag started and how wide the sheet was then; null while no drag runs.
  const drag = useRef<{ x: number; width: number } | null>(null);
  const sheet = () => handle.current?.parentElement ?? undefined;

  // The sheet opens at the width the user left it at. Only the handle changes it after that.
  useLayoutEffect(() => {
    const el = handle.current?.parentElement;
    if (el) {
      setWidth(el, sheetWidth(name));
    }
  }, [name]);

  const reset = () => {
    const el = sheet();
    if (el !== undefined) {
      setWidth(el, null);
    }
    keepSheetWidth(name, null);
  };

  const onPointerDown = (event: PointerEvent<HTMLDivElement>) => {
    const el = sheet();
    if (el === undefined || event.button !== 0) {
      return;
    }
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    drag.current = { width: drawnWidth(el), x: event.clientX };
  };

  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    const el = sheet();
    if (el === undefined || drag.current === null) {
      return;
    }
    // The sheet is at the right edge, so moving the handle left widens it.
    setWidth(el, drag.current.width + drag.current.x - event.clientX);
  };

  const onPointerEnd = (event: PointerEvent<HTMLDivElement>) => {
    const el = sheet();
    if (el === undefined || drag.current === null) {
      return;
    }
    drag.current = null;
    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }
    const dragged = drawnWidth(el);
    setWidth(el, dragged);
    keepSheetWidth(name, dragged);
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key === "Home") {
      event.preventDefault();
      reset();
    }
  };

  return (
    // The handle is last in the sheet, so the focus the sheet takes on opening goes to the header.
    <div
      ref={handle}
      // oxlint-disable-next-line jsx-a11y/prefer-tag-over-role -- a focusable separator is a widget, which `hr` cannot be
      role="separator"
      aria-label="Resize"
      aria-orientation="vertical"
      tabIndex={0}
      className="w-sheet-handle hover:bg-sheet-handle focus-visible:bg-shell-focus-ring active:bg-sheet-handle absolute inset-y-0 left-0 cursor-col-resize touch-none outline-none"
      onDoubleClick={reset}
      onKeyDown={onKeyDown}
      onLostPointerCapture={onPointerEnd}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerEnd}
    />
  );
};

/**
 * A column: the header, then the content, which scrolls down only; the handle on its left edge.
 * `name` is the sheet whose width the drag keeps.
 */
const SheetContent = ({
  name,
  over,
  children,
  ...props
}: OwnProps<typeof SheetPrimitive.Content> & {
  name: SheetName;
  over?: SheetLayer;
}) => (
  <SheetPortal>
    <SheetOverlay over={over} />
    <SheetPrimitive.Content
      data-slot="sheet-content"
      className={cn(SHEET_CONTENT, layerVariants({ over }))}
      {...props}
    >
      {children}
      <ResizeHandle name={name} />
    </SheetPrimitive.Content>
  </SheetPortal>
);

/** The title and the description, with a ghost Close at the top right. */
const SheetHeader = ({ children }: { children: ReactNode }) => (
  <div data-slot="sheet-header" className="flex items-start gap-3">
    <div className="flex min-w-0 flex-1 flex-col gap-1">{children}</div>
    <SheetClose asChild>
      <Button
        variant="ghost"
        size="icon"
        aria-label="Close"
        track="sheet.close"
      >
        <CloseIcon />
      </Button>
    </SheetClose>
  </div>
);

const SheetFooter = ({ ...props }: OwnProps<"div">) => (
  <div
    data-slot="sheet-footer"
    className="mt-auto flex justify-end gap-2"
    {...props}
  />
);

const SheetTitle = ({ ...props }: OwnProps<typeof SheetPrimitive.Title>) => (
  <SheetPrimitive.Title
    data-slot="sheet-title"
    className="type-dialog-title text-dialog-title m-0"
    {...props}
  />
);

/** Under the title, in mono: the name of what the sheet is about. */
const SheetDescription = ({
  ...props
}: OwnProps<typeof SheetPrimitive.Description>) => (
  <SheetPrimitive.Description
    data-slot="sheet-description"
    className="type-technical text-text-caption m-0 truncate"
    {...props}
  />
);

export {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetOverlay,
  SheetPortal,
  SheetTitle,
  SheetTrigger,
};
