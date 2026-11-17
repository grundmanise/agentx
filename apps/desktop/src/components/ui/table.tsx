// shadcn/ui Table (new-york-v4 `table`), on our tokens: DESIGN.md §6 "Table (popover)". Its
// parts keep shadcn's names and `data-slot` attributes and take no `className`; a row's tone and
// a cell's alignment are variants. The registry was out of reach when it was added, so its source
// was copied by hand from shadcn's `registry/new-york-v4/ui/table.tsx`, then adapted.
import { cva } from "class-variance-authority";
import type { VariantProps } from "class-variance-authority";

import type { OwnProps } from "#/lib/utils.ts";

const Table = ({ ...props }: OwnProps<"table">) => (
  <div data-slot="table-container" className="relative w-full overflow-x-auto">
    <table
      data-slot="table"
      className="type-secondary w-full border-collapse"
      {...props}
    />
  </div>
);

const TableHeader = ({ ...props }: OwnProps<"thead">) => (
  <thead data-slot="table-header" {...props} />
);

const TableBody = ({ ...props }: OwnProps<"tbody">) => (
  <tbody data-slot="table-body" {...props} />
);

// `shell.divider` above each row of the body; an `attention` row on `tone.attention.bg`.
const tableRowVariants = cva("", {
  defaultVariants: { tone: "default" },
  variants: {
    tone: {
      attention: "border-shell-divider bg-tone-attention-bg border-t",
      default: "border-shell-divider border-t",
      header: "",
    },
  },
});

const TableRow = ({
  tone,
  ...props
}: OwnProps<"tr"> & VariantProps<typeof tableRowVariants>) => (
  <tr data-slot="table-row" className={tableRowVariants({ tone })} {...props} />
);

const alignVariants = {
  center: "text-center",
  start: "text-left",
};

// A column caption in `table.header-text`.
const tableHeadVariants = cva(
  "text-table-header-text px-2 pb-1.5 align-bottom font-normal wrap-break-word",
  { defaultVariants: { align: "start" }, variants: { align: alignVariants } }
);

const TableHead = ({
  align,
  ...props
}: OwnProps<"th"> & VariantProps<typeof tableHeadVariants>) => (
  <th
    data-slot="table-head"
    className={tableHeadVariants({ align })}
    {...props}
  />
);

const tableCellVariants = cva(
  "text-text-secondary px-2 py-2 align-middle font-normal",
  {
    defaultVariants: { align: "start" },
    variants: { align: alignVariants },
  }
);

/** A body cell; with `scope="row"` it renders a row header (`th`). */
const TableCell = ({
  align,
  scope,
  ...props
}: OwnProps<"td"> &
  VariantProps<typeof tableCellVariants> & { scope?: "row" }) =>
  scope === "row" ? (
    <th
      data-slot="table-cell"
      scope="row"
      className={tableCellVariants({ align })}
      {...props}
    />
  ) : (
    <td
      data-slot="table-cell"
      className={tableCellVariants({ align })}
      {...props}
    />
  );

export { Table, TableBody, TableCell, TableHead, TableHeader, TableRow };
