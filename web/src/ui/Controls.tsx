import {
  ButtonHTMLAttributes,
  CSSProperties,
  FormHTMLAttributes,
  HTMLAttributes,
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
  forwardRef,
  useState
} from "react";

type ButtonVariant = "primary" | "ghost" | "quiet" | "danger";
type ControlSize = "sm" | "md" | "lg";
type Tone = "green" | "amber" | "red" | "blue" | "gray" | "purple";

function cx(...parts: Array<string | false | null | undefined>) {
  return parts.filter(Boolean).join(" ");
}

export function Button({
  variant = "ghost",
  size = "md",
  className = "",
  type = "button",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; size?: ControlSize }) {
  return (
    <button
      type={type}
      className={cx("btn", `tk-button tk-button-${variant}`, size === "sm" ? "btn-sm tk-button-sm" : `tk-button-${size}`, variant === "primary" && "btn-primary", variant === "ghost" && "btn-ghost", variant === "danger" && "danger-lite", className)}
      {...props}
    />
  );
}

export function FilterBar({
  children,
  className = "",
  as = "div",
  ...props
}: ({ children: ReactNode; className?: string; as?: "div" } & HTMLAttributes<HTMLDivElement>) | ({ children: ReactNode; className?: string; as: "form" } & FormHTMLAttributes<HTMLFormElement>)) {
  if (as === "form") {
    return <form className={cx("toolbar tk-filter-bar", className)} {...(props as FormHTMLAttributes<HTMLFormElement>)}>{children}</form>;
  }
  return <div className={cx("toolbar tk-filter-bar", className)} {...(props as HTMLAttributes<HTMLDivElement>)}>{children}</div>;
}

export function BulkActionBar({ children, className = "" }: { children: ReactNode; className?: string }) {
  return <div className={cx("toolbar tk-bulk-action-bar", className)}>{children}</div>;
}

export function FormField({ label, children, className = "" }: { label: ReactNode; children: ReactNode; className?: string }) {
  return (
    <label className={cx("field tk-form-field", className)}>
      <span>{label}</span>
      {children}
    </label>
  );
}

export const SelectField = forwardRef<HTMLSelectElement, SelectHTMLAttributes<HTMLSelectElement> & { label?: ReactNode }>(
  function SelectField({ label, className = "", children, ...props }, ref) {
    const select = (
      <select ref={ref} className={cx("input tk-select-field", className)} {...props}>
        {children}
      </select>
    );
    if (!label) return select;
    return (
      <FormField label={label}>
        {select}
      </FormField>
    );
  }
);

export const CheckboxField = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement> & { label?: ReactNode; wrapperClassName?: string }>(
  function CheckboxField({ label, wrapperClassName = "", className = "", type, ...props }, ref) {
    const input = <input ref={ref} type="checkbox" className={cx("tk-checkbox-field", className)} {...props} />;
    if (!label) return input;
    return <label className={cx("tk-checkbox-label", wrapperClassName)}>{input}<span>{label}</span></label>;
  }
);

export function SwitchField({
  checked,
  onCheckedChange,
  disabled,
  className = "",
  label
}: {
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  disabled?: boolean;
  className?: string;
  label?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-label={label}
      className={cx("switch tk-switch-field", checked && "on", className)}
      disabled={disabled}
      onClick={() => onCheckedChange(!checked)}
    />
  );
}

export function StatusBadge({ children, tone = "gray", dot = true, className = "" }: { children: ReactNode; tone?: Tone; dot?: boolean; className?: string }) {
  return <span className={cx("badge tk-status-badge", dot && "dot", `b-${tone}`, className)}>{children}</span>;
}

export function TrendBars({
  values,
  label = "30-day trend",
  maxBars,
  maxWidth,
  className = ""
}: {
  values: Array<number | null>;
  label?: string;
  maxBars?: number;
  maxWidth?: string;
  className?: string;
}) {
  const barCount = Math.max(1, maxBars ?? Math.min(Math.max(values.length, 1), 30));
  const rawPoints = values.slice(-barCount);
  const points = Array.from({ length: barCount }, (_, index): number | null => {
    const sourceIndex = index - (barCount - rawPoints.length);
    return sourceIndex >= 0 && sourceIndex < rawPoints.length ? rawPoints[sourceIndex] ?? null : null;
  });
  const style = {
    ...(maxWidth ? { "--tk-trend-bar-max": maxWidth } : {})
  } as CSSProperties;
  return (
    <div className={cx("tk-trend-bars model-trend-bars", className)} aria-label={values.length ? label : `${label}: no data`} style={style}>
      {points.map((value, index) => {
        if (value === null) {
          return <i className="empty" key={`empty-${index}`} />;
        }
        const safeValue = Math.max(0, Math.min(100, value));
        const tone = safeValue >= 85 ? "ok" : safeValue >= 65 ? "warn" : "bad";
        return <i className={tone} key={`${value}-${index}`} />;
      })}
    </div>
  );
}

export function CopyButton({
  value,
  children,
  copiedChildren,
  className = "",
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { value: string; copiedChildren?: ReactNode }) {
  const [copied, setCopied] = useState(false);
  async function copy() {
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      const textarea = document.createElement("textarea");
      textarea.value = value;
      textarea.setAttribute("readonly", "");
      textarea.style.position = "fixed";
      textarea.style.opacity = "0";
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand("copy");
      document.body.removeChild(textarea);
    }
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  }
  return (
    <Button
      type="button"
      size="sm"
      variant="ghost"
      className={cx("tk-copy-button", className)}
      onClick={() => void copy()}
      {...props}
    >
      {copied ? copiedChildren || children : children}
    </Button>
  );
}

export function ConfirmAction({
  confirmMessage,
  onConfirm,
  children,
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & { confirmMessage: string; onConfirm: () => void | Promise<void> }) {
  return (
    <Button
      type="button"
      {...props}
      onClick={(event) => {
        props.onClick?.(event);
        if (event.defaultPrevented) return;
        if (window.confirm(confirmMessage)) void onConfirm();
      }}
    >
      {children}
    </Button>
  );
}
