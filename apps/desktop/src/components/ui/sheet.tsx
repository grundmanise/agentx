// shadcn/ui Sheet (`pnpm dlx shadcn@latest add sheet`), on our tokens: DESIGN.md §6 "Sheet".
// The design has one sheet, at the right edge, so there is no `side`. Its parts keep shadcn's
// names and `data-slot` attributes and take no `className`; the close is our ghost Button, in the
// header. A resizable sheet has a handle on its left edge that drags its width.
import { cva } from "class-variance-authority";
import { Dialog as SheetPrimitive } from "radix-ui";
import { useLayoutEffect, useRef } from "react";
import type { KeyboardEvent, PointerEvent, ReactNode } from "react";

import { CloseIcon } from "#/components/icons/close.tsx";
import { Button } from "#/components/ui/button.tsx";
import type { OwnProps } from "#/lib/utils.ts";

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

/** `shell.scrim` behind the sheet. */
const SheetOverlay = ({
  ...props
}: OwnProps<typeof SheetPrimitive.Overlay>) => (
  <SheetPrimitive.Overlay
    data-slot="sheet-overlay"
    className="bg-shell-scrim fixed inset-0 z-40"
    {...props}
  />
);

const sheetContentVariants = cva(
  "rounded-sheet border-sheet-border bg-sheet-bg shadow-overlay top-sheet right-sheet bottom-sheet py-sheet-y px-sheet fixed z-40 flex flex-col gap-6 overflow-hidden border outline-none",
  {
    defaultVariants: { size: "default" },
    variants: {
      size: {
        /** `sheet.width`: the History sheet. */
        default: "w-sheet",
        /**
         * `sheet.log-width`, which the user drags between `sheet.log-min-width` and
         * `sheet.log-max-width`: the Activity log sheet.
         */
        resizable: "w-sheet-dragged min-w-sheet-log max-w-sheet-log",
        /** `sheet.width-wide`: a sheet of diffs, such as What's new. */
        wide: "w-sheet-wide",
      },
    },
  }
);

/** The CSS variable that `w-sheet-dragged` reads, set on the sheet. */
const DRAGGED_WIDTH = "--sheet-dragged-width";
/** How far an arrow key moves the handle, in pixels. */
const KEY_STEP = 40;

/** The width a sheet takes as drawn, which its minimum and maximum width have clamped. */
const drawnWidth = (sheet: HTMLElement) =>
  Math.round(sheet.getBoundingClientRect().width);

/** The sheet a handle is on. */
const sheetOf = (handle: HTMLElement | null) =>
  handle?.parentElement ?? undefined;

/** Sets the width the sheet asks for, or its default when `width` is null. */
const setWidth = (sheet: HTMLElement, width: number | null) => {
  if (width === null) {
    sheet.style.removeProperty(DRAGGED_WIDTH);
  } else {
    sheet.style.setProperty(DRAGGED_WIDTH, `${width}px`);
  }
};

/**
 * The handle on a resizable sheet's left edge, `sheet.handle-width` wide: dragging it moves the
 * sheet's left edge, an arrow key moves it `KEY_STEP` pixels, and a double click or Home brings
 * back the default width. It shows `sheet.handle` on hover, and the focus ring colour on focus.
 * The sheet takes the new width at once; `onWidthChange` hears it, clamped, when the drag ends,
 * and null for the default.
 */
const ResizeHandle = ({
  width,
  onWidthChange,
}: {
  width: number | null;
  onWidthChange: (width: number | null) => void;
}) => {
  const handle = useRef<HTMLDivElement>(null);
  // Where the drag started and how wide the sheet was then; null while no drag runs.
  const drag = useRef<{ x: number; width: number } | null>(null);
  const sheet = () => sheetOf(handle.current);

  // The sheet opens at the width the user left it at.
  useLayoutEffect(() => {
    const el = sheetOf(handle.current);
    if (el !== undefined) {
      setWidth(el, width);
    }
  }, [width]);

  const resize = (el: HTMLElement, next: number) => {
    setWidth(el, next);
    onWidthChange(drawnWidth(el));
  };

  const reset = () => {
    const el = sheet();
    if (el !== undefined) {
      setWidth(el, null);
    }
    onWidthChange(null);
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
    resize(el, drawnWidth(el));
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    const el = sheet();
    if (el === undefined) {
      return;
    }
    if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
      event.preventDefault();
      const step = event.key === "ArrowLeft" ? KEY_STEP : -KEY_STEP;
      resize(el, drawnWidth(el) + step);
    } else if (event.key === "Home") {
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

/** A sheet of a fixed width, or one the user resizes, whose width the caller keeps. */
type SheetSize =
  | { size?: "default" | "wide" }
  | {
      size: "resizable";
      /** The width the user dragged the sheet to, or null for `sheet.log-width`. */
      width: number | null;
      onWidthChange: (width: number | null) => void;
    };

/**
 * `sheet.width` (`sheet.width-wide` for `size="wide"`, the dragged width for
 * `size="resizable"`), inset `sheet.inset` from the top, right and bottom, `sheet.bg` with
 * `sheet.border`, `sheet.radius`, `overlay.shadow` and `sheet.padding`. A column: the header, then
 * the content, which scrolls down only.
 */
const SheetContent = ({
  children,
  ...props
}: Omit<OwnProps<typeof SheetPrimitive.Content>, keyof SheetSize> &
  SheetSize) => {
  if (props.size === "resizable") {
    const { size, width, onWidthChange, ...rest } = props;
    return (
      <SheetPortal>
        <SheetOverlay />
        <SheetPrimitive.Content
          data-slot="sheet-content"
          className={sheetContentVariants({ size })}
          {...rest}
        >
          {children}
          <ResizeHandle width={width} onWidthChange={onWidthChange} />
        </SheetPrimitive.Content>
      </SheetPortal>
    );
  }
  const { size, ...rest } = props;
  return (
    <SheetPortal>
      <SheetOverlay />
      <SheetPrimitive.Content
        data-slot="sheet-content"
        className={sheetContentVariants({ size })}
        {...rest}
      >
        {children}
      </SheetPrimitive.Content>
    </SheetPortal>
  );
};

/** The title and the description, with a ghost Close at the top right. */
const SheetHeader = ({ children }: { children: ReactNode }) => (
  <div data-slot="sheet-header" className="flex items-start gap-3">
    <div className="flex min-w-0 flex-1 flex-col gap-1">{children}</div>
    <SheetClose asChild>
      <Button variant="ghost" size="icon" aria-label="Close">
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
