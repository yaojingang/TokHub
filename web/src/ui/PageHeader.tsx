import { ReactNode } from "react";

export function PageHeader({ description, actions }: { description?: ReactNode; actions?: ReactNode }) {
  if (!description && !actions) return null;
  return (
    <div className="tk-page-header">
      {description ? <div className="tk-page-header-description">{description}</div> : <span />}
      {actions ? <ActionBar>{actions}</ActionBar> : null}
    </div>
  );
}

export function ActionBar({ children, align = "end", className = "" }: { children: ReactNode; align?: "start" | "end"; className?: string }) {
  return <div className={`tk-action-bar ${align === "start" ? "start" : "end"} ${className}`.trim()}>{children}</div>;
}
