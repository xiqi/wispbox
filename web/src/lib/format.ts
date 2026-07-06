import type { Address } from "./types";

export function formatBytes(n?: number): string {
  if (n === undefined || n === null || Number.isNaN(n)) return "—";
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

/** Formats a mailbox quota given in whole megabytes. */
export function formatQuota(mb: number): string {
  if (!mb || mb <= 0) return "No quota";
  return mb >= 1024 ? `${(mb / 1024).toFixed(1)} GB` : `${mb} MB`;
}

/** Human labels for a DnsRecord.status; shared by the DNS section and setup. */
export const dnsStatusLabel: Record<string, string> = {
  ok: "Found",
  missing: "Missing",
  mismatch: "Mismatch",
  unknown: "Not checked",
  "": "Not checked",
};

export function formatDate(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/**
 * Gmail-style relative timestamps for list views. Returns "" (not "—") for a
 * missing/invalid date on purpose: an empty cell reads better in dense message
 * rows than an em-dash.
 */
export function formatWhen(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) {
    return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  }
  if (d.getFullYear() === now.getFullYear()) {
    return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  }
  return d.toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
}

export function senderLabel(addrs: Address[] | null | undefined): string {
  if (!addrs || addrs.length === 0) return "(unknown)";
  const a = addrs[0];
  return a.name || a.email || "(unknown)";
}

export function addressLine(addrs: Address[] | null | undefined): string {
  if (!addrs || addrs.length === 0) return "";
  return addrs.map((a) => (a.name ? `${a.name} <${a.email}>` : a.email)).join(", ");
}

export function daysUntil(iso?: string): number | null {
  if (!iso) return null;
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return null;
  return Math.round((d.getTime() - Date.now()) / 86_400_000);
}

export function uptimeLabel(seconds: number): string {
  if (seconds < 90) return `${seconds}s`;
  if (seconds < 5400) return `${Math.round(seconds / 60)}m`;
  if (seconds < 129_600) return `${Math.round(seconds / 3600)}h`;
  return `${Math.round(seconds / 86_400)}d`;
}
