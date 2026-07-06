import { ReactNode, useEffect, useMemo, useState } from "react";
import {
  ColumnDef,
  flexRender,
  getCoreRowModel,
  getPaginationRowModel,
  useReactTable
} from "@tanstack/react-table";
import { useTranslation } from "react-i18next";
import { Pagination } from "./Pagination";

export type DataTableColumn<T> = ColumnDef<T, unknown> & {
  meta?: {
    align?: "start" | "center" | "end";
    className?: string;
    headerClassName?: string;
    priority?: 1 | 2 | 3 | 4 | 5;
    truncate?: boolean;
    width?: string;
    wrap?: "nowrap" | "normal" | "break";
  };
};

export function DataTable<T>({
  data,
  columns,
  loading = false,
  pageSize = 10,
  cardClassName = "",
  tableClassName = "",
  wrapClassName = "",
  empty,
  loadingText,
  footerNote,
  rowKey,
  getRowClassName
}: {
  data: T[];
  columns: DataTableColumn<T>[];
  loading?: boolean;
  pageSize?: number;
  cardClassName?: string;
  tableClassName?: string;
  wrapClassName?: string;
  empty?: ReactNode;
  loadingText?: string;
  footerNote?: string;
  rowKey?: (row: T, index: number) => string;
  getRowClassName?: (row: T, index: number) => string | undefined;
}) {
  const { t } = useTranslation("common");
  const [pageIndex, setPageIndex] = useState(0);
  const table = useReactTable({
    data,
    columns: columns as ColumnDef<T, unknown>[],
    state: {
      pagination: { pageIndex, pageSize }
    },
    onPaginationChange: (updater) => {
      const next = typeof updater === "function" ? updater({ pageIndex, pageSize }) : updater;
      setPageIndex(next.pageIndex);
    },
    getCoreRowModel: getCoreRowModel(),
    getPaginationRowModel: getPaginationRowModel()
  });

  const totalPages = Math.max(1, table.getPageCount());
  const safePageIndex = Math.min(pageIndex, totalPages - 1);

  useEffect(() => {
    if (safePageIndex !== pageIndex) setPageIndex(safePageIndex);
  }, [pageIndex, safePageIndex]);

  const headerGroups = table.getHeaderGroups();
  const rows = table.getRowModel().rows;
  const columnCount = useMemo(() => columns.length || 1, [columns.length]);

  return (
    <div className={`card board tk-data-table-card ${cardClassName}`.trim()}>
      <div className={`dt-wrap tk-data-table-wrap ${wrapClassName}`.trim()}>
        <table className={`dt tk-data-table ${tableClassName}`.trim()}>
          <colgroup>
            {columns.map((column, index) => {
              const meta = column.meta;
              return <col key={column.id?.toString() || index} style={meta?.width ? { width: meta.width } : undefined} />;
            })}
          </colgroup>
          <thead>
            {headerGroups.map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => {
                  const meta = header.column.columnDef.meta as DataTableColumn<T>["meta"];
                  return (
                    <th className={columnClass(meta, meta?.headerClassName)} key={header.id}>
                      {header.isPlaceholder ? null : flexRender(header.column.columnDef.header, header.getContext())}
                    </th>
                  );
                })}
              </tr>
            ))}
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={columnCount}>{loadingText || t("table.loading")}</td></tr>
            ) : rows.length ? rows.map((row) => (
              <tr key={rowKey ? rowKey(row.original, row.index) : row.id} className={getRowClassName?.(row.original, row.index)}>
                {row.getVisibleCells().map((cell) => {
                  const meta = cell.column.columnDef.meta as DataTableColumn<T>["meta"];
                  return (
                    <td className={columnClass(meta, meta?.className)} key={cell.id}>
                      {flexRender(cell.column.columnDef.cell, cell.getContext())}
                    </td>
                  );
                })}
              </tr>
            )) : (
              <tr>
                <td colSpan={columnCount}>
                  {empty || <div className="empty-state"><h4>{t("table.emptyTitle")}</h4><p>{t("table.emptyHint")}</p></div>}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
      <Pagination
        page={safePageIndex + 1}
        totalPages={totalPages}
        pageSize={pageSize}
        total={data.length}
        note={footerNote}
        onPageChange={(page) => setPageIndex(page - 1)}
      />
    </div>
  );
}

function columnClass<T>(meta?: DataTableColumn<T>["meta"], extra?: string) {
  return [
    extra,
    meta?.align ? `tk-align-${meta.align}` : "",
    meta?.truncate ? "tk-cell-truncate" : "",
    meta?.wrap ? `tk-cell-wrap-${meta.wrap}` : "",
    meta?.priority ? `tk-col-priority-${meta.priority}` : ""
  ].filter(Boolean).join(" ") || undefined;
}
