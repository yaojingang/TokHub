import { ChangeEvent } from "react";
import { useTranslation } from "react-i18next";

export function Pagination({
  page,
  totalPages,
  pageSize,
  total,
  onPageChange,
  note
}: {
  page: number;
  totalPages: number;
  pageSize: number;
  total: number;
  onPageChange: (page: number) => void;
  note?: string;
}) {
  const { t } = useTranslation("common");
  const safeTotalPages = Math.max(1, totalPages);
  const currentPage = Math.min(Math.max(1, page), safeTotalPages);

  function jump(event: ChangeEvent<HTMLInputElement>) {
    const nextPage = Number(event.target.value);
    if (!Number.isFinite(nextPage)) return;
    onPageChange(Math.min(safeTotalPages, Math.max(1, nextPage)));
  }

  return (
    <div className="tfoot tk-pagination" role="navigation" aria-label={t("table.pagination")}>
      <div className="tk-pagination-copy">
        <span>
          {t("table.total", { total })} · {t("table.pageSize", { pageSize })} · {t("table.page", { page: currentPage, totalPages: safeTotalPages })}
        </span>
        {note ? <span>{note}</span> : null}
      </div>
      <div className="tk-pagination-actions">
        <button className="btn btn-ghost btn-sm" disabled={currentPage <= 1} onClick={() => onPageChange(Math.max(1, currentPage - 1))}>
          {t("actions.previous")}
        </button>
        <label className="page-jump">
          <span>{t("table.jumpTo")}</span>
          <input className="input compact-number" aria-label={t("table.pageNumber")} type="number" min={1} max={safeTotalPages} value={currentPage} onChange={jump} />
          <span>{t("table.pageSuffix")}</span>
        </label>
        <button className="btn btn-ghost btn-sm" disabled={currentPage >= safeTotalPages} onClick={() => onPageChange(Math.min(safeTotalPages, currentPage + 1))}>
          {t("actions.next")}
        </button>
      </div>
    </div>
  );
}
