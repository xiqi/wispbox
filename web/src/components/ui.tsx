// wispbox shared UI kit: small, composable, quietly premium.
import {
  type ButtonHTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
  type SelectHTMLAttributes,
  type TextareaHTMLAttributes,
  useEffect,
  useState,
} from "react";
import { Check, Copy, Loader2, Moon, Sun, X } from "lucide-react";
import { useBrand } from "../lib/brand";
import { currentTheme, toggleTheme, type Theme } from "../lib/theme";

/* ---------- Logo ---------- */

export function WispMark({ size = 22, className = "" }: { size?: number; className?: string }) {
  return (
    <svg className={className} width={size * 1.66} height={size} viewBox="76 105 456 275" aria-hidden fill="none">
      <path
        d="M271 191.5L294 191.5L307 196.5L312.5 202L348.5 265L358 278.5L360 278.5L365.5 271L406.5 202L412 195.5L416 193.5L475 193.5L477.5 198L389.5 347L378 362.5L373 365.5L353 366.5L339 361.5L332.5 355L288.5 280L281 271.5L228.5 358L216 367.5L205 368.5L195 367.5L183.5 360L94.5 211L87.5 198L87.5 194L150 193.5L155 196.5L198.5 270L202.5 276L207 278.5L253.5 201L259 195.5Z"
        fill="var(--accent-strong)"
      />
      <path
        d="M462 116.5L517 116.5L520.5 120L518.5 128L495.5 167L489 174.5L472 176.5L430 175.5L427.5 172L428.5 167L453.5 124Z"
        fill="color-mix(in srgb, var(--accent) 35%, white)"
      />
    </svg>
  );
}

export function BrandMark({ size = 22, className = "" }: { size?: number; className?: string }) {
  const brand = useBrand();
  if (brand.logo) {
    return (
      <img
        src={brand.logo}
        alt=""
        aria-hidden
        className={`shrink-0 object-contain ${className}`}
        style={{ width: size * 1.66, height: size }}
      />
    );
  }
  return <WispMark size={size} className={className} />;
}

export function Wordmark({ sub }: { sub?: string }) {
  const brand = useBrand();
  return (
    <div className="flex min-w-0 items-center gap-2.5 select-none">
      <BrandMark />
      <span className="min-w-0 truncate text-[17px] font-semibold text-ink">
        {brand.name}
        {sub && <span className="ml-2 font-normal text-muted">{sub}</span>}
      </span>
    </div>
  );
}

/* ---------- Buttons ---------- */

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "primary" | "ghost" | "outline" | "danger";
  size?: "sm" | "md";
  busy?: boolean;
};

export function Button({
  variant = "outline",
  size = "md",
  busy,
  className = "",
  children,
  disabled,
  ...rest
}: ButtonProps) {
  const base =
    "inline-flex items-center justify-center gap-1.5 rounded-lg font-medium leading-none transition-all duration-150 disabled:opacity-45 disabled:pointer-events-none whitespace-nowrap";
  const sizes = size === "sm" ? "h-7 px-2.5 text-[12.5px]" : "h-9 px-3.5 text-[13.5px]";
  const variants = {
    primary:
      "bg-accent text-accent-ink hover:bg-accent-strong shadow-[0_0_16px_var(--glow)] hover:shadow-[0_0_24px_var(--glow)]",
    outline: "border border-line-strong text-ink hover:bg-inset hover:border-line-strong",
    ghost: "text-muted hover:text-ink hover:bg-inset",
    danger: "border border-danger/40 text-danger hover:bg-danger-dim",
  }[variant];
  return (
    <button className={`${base} ${sizes} ${variants} ${className}`} disabled={disabled || busy} {...rest}>
      {busy && <Loader2 size={14} className="animate-spin" />}
      {children}
    </button>
  );
}

export function ThemeToggle() {
  const [theme, setTheme] = useState<Theme>(currentTheme());
  return (
    <button
      type="button"
      aria-label={theme === "dark" ? "Switch to light" : "Switch to dark"}
      className="inline-flex h-8 w-8 items-center justify-center rounded-lg bg-transparent text-muted transition-none hover:bg-transparent hover:text-ink active:bg-transparent focus-visible:bg-transparent"
      onClick={() => setTheme(toggleTheme())}
      title={theme === "dark" ? "Switch to light" : "Switch to dark"}
    >
      {theme === "dark" ? <Sun size={15} /> : <Moon size={15} />}
    </button>
  );
}

// IconButton is the shared icon-only action button for toolbars, rows, and
// chrome. Pass tone="danger" for destructive actions; revealOnRowHover works
// on rows wrapped in a `group`.
export function IconButton({
  icon,
  tone = "default",
  size = "sm",
  revealOnRowHover,
  className = "",
  type = "button",
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  icon: ReactNode;
  tone?: "default" | "accent" | "danger";
  size?: "sm" | "md";
  revealOnRowHover?: boolean;
}) {
  const tones = {
    default: "text-faint hover:bg-inset hover:text-ink",
    accent: "text-faint hover:bg-inset hover:text-accent",
    danger: "text-faint hover:bg-danger-dim hover:text-danger",
  }[tone];
  const sizes = size === "md" ? "h-8 w-8 rounded-lg" : "h-7 w-7 rounded-md";
  const reveal = revealOnRowHover ? "opacity-0 group-hover:opacity-100" : "";
  return (
    <button
      type={type}
      className={`inline-flex shrink-0 items-center justify-center transition-all disabled:pointer-events-none disabled:opacity-45 ${sizes} ${tones} ${reveal} ${className}`}
      {...rest}
    >
      {icon}
    </button>
  );
}

/* ---------- Form fields ---------- */

export function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ReactNode;
}) {
  return (
    <label className="block">
      <div className="mb-1.5 text-[12.5px] font-medium text-muted">{label}</div>
      {children}
      {hint && <div className="mt-1.5 text-[12px] leading-relaxed text-faint">{hint}</div>}
    </label>
  );
}

export function Input({ className = "", ...rest }: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={`h-9 w-full rounded-lg border border-line bg-inset px-3 text-[13.5px] leading-none text-ink placeholder:text-faint focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25 transition-shadow ${className}`}
      {...rest}
    />
  );
}

export function TextArea({ className = "", ...rest }: TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={`w-full rounded-lg border border-line bg-inset px-3 py-2.5 text-[13.5px] leading-relaxed text-ink placeholder:text-faint focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25 transition-shadow ${className}`}
      {...rest}
    />
  );
}

export function Select({ className = "", children, ...rest }: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={`h-9 w-full appearance-none rounded-lg border border-line bg-inset px-3 text-[13.5px] leading-none text-ink focus:border-accent/50 focus:outline-none focus:ring-2 focus:ring-accent/25 ${className}`}
      {...rest}
    >
      {children}
    </select>
  );
}

export function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className="group inline-flex h-8 align-middle items-center gap-2 rounded-lg px-1.5 leading-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent/35"
    >
      <span
        className={`relative h-5 w-10 shrink-0 overflow-hidden rounded-full transition-colors ${checked ? "bg-accent" : "bg-line-strong"}`}
      >
        <span
          className={`absolute left-0.5 top-0.5 h-4 w-4 rounded-full bg-white shadow transition-transform ${checked ? "translate-x-5" : "translate-x-0"}`}
        />
      </span>
      {label && (
        <span className="flex h-5 min-w-[4.25rem] items-center whitespace-nowrap text-left text-[13px] leading-none text-muted group-hover:text-ink">
          {label}
        </span>
      )}
    </button>
  );
}

/* ---------- Text ---------- */

export function Identifier({
  children,
  muted,
  className = "",
}: {
  children: ReactNode;
  muted?: boolean;
  className?: string;
}) {
  return (
    <span className={`font-mono text-[12.5px] leading-none ${muted ? "text-muted" : "text-ink"} ${className}`}>
      {children}
    </span>
  );
}

/* ---------- Status ---------- */

const pillTones: Record<string, string> = {
  ok: "text-ok",
  active: "text-ok",
  warn: "text-warn",
  pending: "text-warn",
  dns_wait: "text-warn",
  issuing: "text-info",
  missing: "text-danger",
  mismatch: "text-warn",
  error: "text-danger",
  inactive: "text-danger",
  unknown: "text-faint",
  direct: "text-info",
};

export function StatusPill({ status, label }: { status: string; label?: string }) {
  const tone = pillTones[status] ?? "text-muted";
  return (
    <span
      className={`inline-flex h-6 items-center gap-1.5 rounded-full border border-line bg-inset px-2 text-[11.5px] font-medium leading-none ${tone}`}
    >
      <span className="h-1.5 w-1.5 rounded-full bg-current" />
      {label ?? status.replaceAll("_", " ")}
    </span>
  );
}

/* ---------- Surfaces ---------- */

export function Card({
  title,
  actions,
  children,
  className = "",
}: {
  title?: ReactNode;
  actions?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`rounded-xl border border-line bg-raised ${className}`}>
      {(title || actions) && (
        <header className="flex items-center justify-between gap-3 border-b border-line px-4 py-3">
          <h3 className="text-[13px] font-semibold text-ink">{title}</h3>
          {actions && <div className="flex items-center gap-2">{actions}</div>}
        </header>
      )}
      <div className="p-4">{children}</div>
    </section>
  );
}

export function Modal({
  title,
  onClose,
  children,
  wide,
}: {
  title: string;
  onClose: () => void;
  children: ReactNode;
  wide?: boolean;
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);
  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center overflow-y-auto bg-black/55 p-4 pt-[9vh] backdrop-blur-[2px]"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <div
        className={`animate-rise w-full ${wide ? "max-w-2xl" : "max-w-md"} rounded-xl border border-line bg-overlay shadow-pop`}
      >
        <header className="flex items-center justify-between border-b border-line px-5 py-3.5">
          <h2 className="text-[14px] font-semibold text-ink">{title}</h2>
          <IconButton
            title="Close"
            onClick={onClose}
            icon={<X size={15} />}
          />
        </header>
        <div className="p-5">{children}</div>
      </div>
    </div>
  );
}

export function ConfirmDialog({
  title,
  confirmLabel,
  onClose,
  onConfirm,
  danger,
  closeAfterConfirm = true,
  children,
}: {
  title: string;
  confirmLabel: string;
  onClose: () => void;
  onConfirm: () => Promise<void> | void;
  danger?: boolean;
  closeAfterConfirm?: boolean;
  children?: ReactNode;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function confirm() {
    setBusy(true);
    setError("");
    try {
      await onConfirm();
      if (closeAfterConfirm) onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Action failed");
      setBusy(false);
    }
  }

  return (
    <Modal title={title} onClose={busy ? () => {} : onClose}>
      <div className="space-y-4">
        {children && (
          <div className="rounded-lg border border-line bg-inset px-3 py-2.5 text-[12.5px] leading-relaxed text-muted">
            {children}
          </div>
        )}
        <ErrorNote>{error}</ErrorNote>
        <div className="flex justify-end gap-2">
          <Button type="button" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button type="button" variant={danger ? "danger" : "primary"} busy={busy} onClick={confirm}>
            {confirmLabel}
          </Button>
        </div>
      </div>
    </Modal>
  );
}

export function ErrorNote({ children }: { children: ReactNode }) {
  if (!children) return null;
  return (
    <div className="rounded-lg border border-danger/30 bg-danger-dim px-3 py-2.5 text-[13px] leading-relaxed text-danger">
      {children}
    </div>
  );
}

export function InfoNote({ children }: { children: ReactNode }) {
  return (
    <div className="rounded-lg border border-line bg-inset px-3 py-2.5 text-[12.5px] leading-relaxed text-muted">
      {children}
    </div>
  );
}

export function EmptyState({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="flex flex-col items-center justify-center gap-1.5 py-14 text-center">
      <BrandMark size={26} />
      <div className="mt-2 text-[13.5px] font-medium text-muted">{title}</div>
      {hint && <div className="max-w-sm text-[12.5px] text-faint">{hint}</div>}
    </div>
  );
}

export function Spinner({ label }: { label?: string }) {
  return (
    <div className="flex items-center justify-center gap-2 py-12 text-muted">
      <Loader2 size={16} className="animate-spin" />
      {label && <span className="text-[13px]">{label}</span>}
    </div>
  );
}

/* ---------- Copy button ---------- */

export function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-inset hover:text-ink"
      title="Copy"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text);
        } catch {
          const ta = document.createElement("textarea");
          ta.value = text;
          document.body.appendChild(ta);
          ta.select();
          document.execCommand("copy");
          ta.remove();
        }
        setCopied(true);
        setTimeout(() => setCopied(false), 1200);
      }}
    >
      {copied ? <Check size={14} className="text-ok" /> : <Copy size={13} />}
    </button>
  );
}

/* ---------- Table ---------- */

export function Table({ head, children }: { head: ReactNode; children: ReactNode }) {
  return (
    <div className="-mb-4 overflow-x-auto">
      <table className="w-full border-collapse text-left">
        <thead>
          <tr className="border-b border-line text-[11.5px] uppercase text-faint">
            {head}
          </tr>
        </thead>
        <tbody className="divide-y divide-line">{children}</tbody>
      </table>
    </div>
  );
}

export function Th({ children, className = "" }: { children?: ReactNode; className?: string }) {
  return <th className={`px-3 py-3 align-middle font-medium leading-none ${className}`}>{children}</th>;
}

export function Td({ children, className = "" }: { children?: ReactNode; className?: string }) {
  const justify = className.includes("text-right")
    ? "justify-items-end text-right"
    : className.includes("text-center")
      ? "justify-items-center text-center"
      : "justify-items-start text-left";
  return (
    <td className={`px-3 py-2.5 align-middle text-[13px] leading-none ${className}`}>
      <div className={`grid min-h-8 w-full content-center ${justify}`}>{children}</div>
    </td>
  );
}

/* ---------- Toast ---------- */

type ToastMsg = { id: number; kind: "ok" | "error"; text: string };
let toastListener: ((t: ToastMsg) => void) | null = null;
let toastId = 0;

export function toast(text: string, kind: "ok" | "error" = "ok") {
  toastListener?.({ id: ++toastId, kind, text });
}

export function ToastHost() {
  const [items, setItems] = useState<ToastMsg[]>([]);
  useEffect(() => {
    toastListener = (t) => {
      setItems((prev) => [...prev, t]);
      setTimeout(() => setItems((prev) => prev.filter((x) => x.id !== t.id)), 4200);
    };
    return () => {
      toastListener = null;
    };
  }, []);
  return (
    <div className="pointer-events-none fixed bottom-5 left-1/2 z-[60] flex -translate-x-1/2 flex-col items-center gap-2">
      {items.map((t) => (
        <div
          key={t.id}
          className={`animate-rise pointer-events-auto max-w-md rounded-lg border px-4 py-2.5 text-[13px] shadow-pop backdrop-blur ${
            t.kind === "error"
              ? "border-danger/40 bg-danger-dim text-danger"
              : "border-line bg-overlay text-ink"
          }`}
        >
          {t.text}
        </div>
      ))}
    </div>
  );
}
