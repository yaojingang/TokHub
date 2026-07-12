import { ReactNode } from "react";
import * as DialogPrimitive from "@radix-ui/react-dialog";

export function Dialog({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="tk-dialog-overlay" />
        <DialogPrimitive.Content className="tk-dialog-content">
          <div className="tk-dialog-head">
            <div>
              <DialogPrimitive.Title className="tk-dialog-title">{title}</DialogPrimitive.Title>
              {description ? <DialogPrimitive.Description className="tk-dialog-description">{description}</DialogPrimitive.Description> : null}
            </div>
            <DialogPrimitive.Close className="icon-btn" aria-label="Close">×</DialogPrimitive.Close>
          </div>
          <div className="tk-dialog-body">{children}</div>
          {footer ? <div className="tk-dialog-footer">{footer}</div> : null}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

export function Drawer({
  open,
  onOpenChange,
  title,
  description,
  children,
  footer,
  className = ""
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
  className?: string;
}) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="drawer-mask open tk-drawer-overlay" />
        <DialogPrimitive.Content className={`drawer open private-editor tk-drawer-content ${className}`.trim()}>
          <div className="dh">
            <div>
              <DialogPrimitive.Title>{title}</DialogPrimitive.Title>
              {description ? <DialogPrimitive.Description>{description}</DialogPrimitive.Description> : null}
            </div>
            <DialogPrimitive.Close className="icon-btn" aria-label="Close">×</DialogPrimitive.Close>
          </div>
          <div className="db form-grid">{children}</div>
          {footer ? <div className="df">{footer}</div> : null}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}
