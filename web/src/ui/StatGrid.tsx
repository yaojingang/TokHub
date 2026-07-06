import { ReactNode } from "react";

export type StatItem = {
  label: ReactNode;
  value: ReactNode;
  hint?: ReactNode;
  tone?: "green" | "amber" | "red" | "blue" | "gray";
};

export function StatGrid({ items, className = "" }: { items: StatItem[]; className?: string }) {
  return (
    <div className={`stat-row tk-stat-grid ${className}`.trim()}>
      {items.map((item, index) => (
        <StatCard label={item.label} value={item.value} hint={item.hint} key={index} />
      ))}
    </div>
  );
}

export function StatCard({ label, value, hint, tone }: StatItem) {
  return (
    <div className={`stat tk-stat-card ${tone ? `tone-${tone}` : ""}`}>
      <div className="l">{label}</div>
      <div className="v">{value}</div>
      {hint ? <div className="d">{hint}</div> : null}
    </div>
  );
}
