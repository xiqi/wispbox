import { get } from "../../../lib/api";
import { useLoad } from "../../../lib/hooks";
import type { Overview as OverviewData } from "../../../lib/types";
import { daysUntil, formatBytes, formatDate, uptimeLabel } from "../../../lib/format";
import {
  Card,
  EmptyState,
  ErrorNote,
  Identifier,
  Spinner,
  StatusPill,
  Table,
  Td,
  Th,
} from "../../../components/ui";

export default function Overview() {
  const { data, error, busy } = useLoad(() => get<OverviewData>("/api/admin/overview"));

  if (busy && !data) return <Spinner />;
  if (error) return <ErrorNote>{error}</ErrorNote>;
  if (!data) return null;

  const memTotal = data.system_memory.total_bytes;
  const memAvail = data.system_memory.available_bytes;

  return (
    <div className="space-y-5">
      {data.mode === "development" && (
        <div className="rounded-lg border border-warn/30 bg-warn/10 px-3 py-2 text-[12.5px] text-warn">
          Development mode — Postfix, Dovecot, DNS, and certificates are mocked. Nothing here
          touches a real mail server.
        </div>
      )}

      {/* stat strip */}
      <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <Stat
          label="Services"
          value={
            Object.values(data.services).every(Boolean)
              ? "all running"
              : Object.entries(data.services)
                  .filter(([, ok]) => !ok)
                  .map(([name]) => name)
                  .join(", ") + " down"
          }
          tone={Object.values(data.services).every(Boolean) ? "ok" : "bad"}
          sub={`up ${uptimeLabel(data.uptime_seconds)}`}
        />
        <Stat
          label="Memory"
          value={
            memTotal && memAvail
              ? `${formatBytes(memTotal - memAvail)} / ${formatBytes(memTotal)}`
              : formatBytes(data.process_memory.sys_bytes)
          }
          sub={memTotal ? "system used" : "wispboxd process"}
        />
        <Stat
          label="Disk"
          value={
            data.disk.total_bytes
              ? `${formatBytes(data.disk.used_bytes)} / ${formatBytes(data.disk.total_bytes)}`
              : "—"
          }
          sub={data.disk.free_bytes ? `${formatBytes(data.disk.free_bytes)} free` : ""}
          tone={
            data.disk.total_bytes && data.disk.free_bytes! < data.disk.total_bytes * 0.1
              ? "bad"
              : undefined
          }
        />
        <Stat
          label="Mail queue"
          value={String(data.queue_count)}
          sub={data.queue_count === 1 ? "message waiting" : "messages waiting"}
          tone={data.queue_count > 20 ? "bad" : undefined}
        />
      </div>

      {/* services detail */}
      <div className="grid gap-5 lg:grid-cols-2">
        <Card title="Domain health">
          {data.domains.length === 0 ? (
            <EmptyState title="No domains yet" />
          ) : (
            <>
              <div className="divide-y divide-line md:hidden">
                {data.domains.map((d) => (
                  <div key={d.id} className="space-y-2 py-3 first:pt-0 last:pb-0">
                    <Identifier>{d.name}</Identifier>
                    <div className="flex flex-wrap gap-2">
                      <StatusPill status={d.status} />
                      <StatusPill
                        status={d.cert_status ?? "none"}
                        label={`Certificate: ${formatStatusText(d.cert_status ?? "none")}`}
                      />
                      <StatusPill status={d.delivery_mode ?? "direct"} label={formatStatusText(d.delivery_mode ?? "direct")} />
                    </div>
                  </div>
                ))}
              </div>
              <div className="hidden md:block">
                <Table
                  head={
                    <>
                      <Th>Domain</Th>
                      <Th>Status</Th>
                      <Th>Certificate</Th>
                      <Th>Delivery</Th>
                    </>
                  }
                >
                  {data.domains.map((d) => (
                    <tr key={d.id}>
                      <Td>
                        <Identifier>{d.name}</Identifier>
                      </Td>
                      <Td>
                        <StatusPill status={d.status} />
                      </Td>
                      <Td>
                        <StatusPill status={d.cert_status ?? "none"} />
                      </Td>
                      <Td className="text-muted">{formatStatusText(d.delivery_mode ?? "direct")}</Td>
                    </tr>
                  ))}
                </Table>
              </div>
            </>
          )}
        </Card>

        <Card title="Certificates">
          {data.certificates.length === 0 ? (
            <EmptyState title="No certificates tracked" />
          ) : (
            <>
              <div className="divide-y divide-line md:hidden">
                {data.certificates.map((c) => {
                  const days = daysUntil(c.not_after);
                  return (
                    <div key={c.id} className="space-y-2 py-3 first:pt-0 last:pb-0">
                      <Identifier className="block break-all">{c.hostname}</Identifier>
                      <div className="flex flex-wrap items-center gap-2">
                        <StatusPill status={c.status} />
                        <span className={days !== null && days < 14 ? "text-[12.5px] text-warn" : "text-[12.5px] text-muted"}>
                          {days === null ? "expires unknown" : `expires in ${days}d`}
                        </span>
                      </div>
                    </div>
                  );
                })}
              </div>
              <div className="hidden md:block">
                <Table
                  head={
                    <>
                      <Th>Hostname</Th>
                      <Th>Status</Th>
                      <Th>Expires</Th>
                    </>
                  }
                >
                  {data.certificates.map((c) => {
                    const days = daysUntil(c.not_after);
                    return (
                      <tr key={c.id}>
                        <Td>
                          <Identifier>{c.hostname}</Identifier>
                        </Td>
                        <Td>
                          <StatusPill status={c.status} />
                        </Td>
                        <Td className={days !== null && days < 14 ? "text-warn" : "text-muted"}>
                          {days === null ? "—" : `${days}d`}
                        </Td>
                      </tr>
                    );
                  })}
                </Table>
              </div>
            </>
          )}
        </Card>
      </div>

      <Card title="Recent delivery & service errors">
        {data.recent_errors.length === 0 ? (
          <div className="py-4 text-center text-[13px] text-faint">
            No recent errors.
          </div>
        ) : (
          <ul className="space-y-2">
            {data.recent_errors.map((e) => (
              <li key={e.id} className="rounded-lg border border-danger/20 bg-danger-dim/60 px-3 py-2">
                <div className="flex items-center justify-between gap-2 text-[12px]">
                  <span className="font-medium text-danger">
                    {e.service} · {e.event_type}
                  </span>
                  <span className="text-faint">{formatDate(e.created_at)}</span>
                </div>
                <div className="mt-1 break-words font-mono text-[12px] text-muted">{e.message}</div>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

function formatStatusText(value: string): string {
  if (value === "dns_wait") return "DNS wait";
  if (value === "none") return "Not issued";
  return value ? value.replaceAll("_", " ").replace(/^\w/, (c) => c.toUpperCase()) : "—";
}

function Stat({
  label,
  value,
  sub,
  tone,
}: {
  label: string;
  value: string;
  sub?: string;
  tone?: "ok" | "bad";
}) {
  return (
    <div className="rounded-xl border border-line bg-raised px-4 py-3.5">
      <div className="text-[11px] font-medium text-faint">{label}</div>
      <div
        className={`mt-1 truncate text-[17px] font-semibold ${
          tone === "ok" ? "text-ok" : tone === "bad" ? "text-danger" : "text-ink"
        }`}
      >
        {value}
      </div>
      {sub && <div className="mt-0.5 text-[11.5px] text-faint">{sub}</div>}
    </div>
  );
}
