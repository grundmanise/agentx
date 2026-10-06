import { cva, type VariantProps } from "class-variance-authority";
import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

// DESIGN.md §6 "Buttons". Disabled is 30% opacity and inert, never a grey fill.
const buttonVariants = cva(
  "inline-flex shrink-0 items-center justify-center gap-2 whitespace-nowrap rounded-button text-[14px] transition-opacity duration-200 disabled:pointer-events-none disabled:opacity-30",
  {
    variants: {
      variant: {
        primary: "bg-button-primary-bg font-semibold text-button-primary-text hover:bg-button-primary-hover",
        secondary: "bg-button-secondary-bg font-medium text-button-secondary-text hover:bg-button-secondary-hover",
        ghost: "bg-transparent font-medium text-button-ghost-text hover:bg-button-ghost-hover hover:text-text-primary",
        danger: "bg-transparent font-medium text-button-danger-text hover:bg-button-danger-hover",
      },
      size: {
        default: "h-[38px] px-4",
        sm: "h-[34px] px-3.5 text-[13.5px]",
        icon: "size-8 p-0",
      },
    },
    defaultVariants: { variant: "secondary", size: "default" },
  },
);

export function Button({
  className,
  variant,
  size,
  type = "button",
  ...props
}: ComponentProps<"button"> & VariantProps<typeof buttonVariants>) {
  return <button type={type} className={cn(buttonVariants({ variant, size }), className)} {...props} />;
}
