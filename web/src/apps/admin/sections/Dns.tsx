import { useState } from "react";
import { RefreshCw } from "lucide-react";
import { get, post } from "../../../lib/api";
import { dnsStatusLabel } from "../../../lib/format";
import { useLoad } from "../../../lib/hooks";
import type { DnsRecord, Domain } from "../../../lib/types";
import {
  Button,
  Card,
  CopyButton,
  EmptyState,
  ErrorNote,
  Select,
  Spinner,
  StatusPill,
  toast,
} from "../../../components/ui";

export default function Dns() {
  const domains = useLoad(() => get<{ domains: Domain[] }>("/api/admin/domains"));
  const [domainID, setDomainID] = useState<number>(0);
  const effectiveID = domainID || domains.data?.domains[0]?.id || 0;

  const records = useLoad(
    async () =>
      effectiveID
        ? get<{ records: DnsRecord[] }>(`/api/admin/dns/${effectiveID}`)
        : { records: [] as DnsRecord[] },
    [effectiveID],
  );
  const [checking, setChecking] = useState(false);

  async function checkNow() {
    if (!effectiveID) return;
    setChecking(true);
    try {
      const res = await post<{ records: DnsRecord[] }>(`/api/admin/dns/${effectiveID}/check`);
      records.setData(res);
      const bad = res.records.filter((r) => r.status !== "ok").length;
      toast(bad === 0 ? "All DNS records look good" : `${bad} record(s) need attention`);
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setChecking(false);
    }
  }

  if (domains.busy && !domains.data) return <Spinner />;
  if (domains.data && domains.data.domains.length === 0) {
    return <EmptyState title="No domains yet" />;
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="w-64">
          <Select value={effectiveID} onChange={(e) => setDomainID(Number(e.target.value))}>
            {domains.data?.domains.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </Select>
        </div>
        <Button variant="primary" size="sm" onClick={checkNow} busy={checking}>
          <RefreshCw size={13} /> Check again
        </Button>
      </div>

      {records.error && <ErrorNote>{records.error}</ErrorNote>}
      {records.busy && !records.data && <Spinner />}

      <div className="space-y-3">
        {records.data?.records.map((r) => (
          <Card key={r.purpose + r.name} className="overflow-hidden">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="flex items-center gap-2.5">
                <span className="rounded-md border border-line bg-inset px-2 py-0.5 font-mono text-[11.5px] font-semibold text-ink">
                  {r.type}
                </span>
                <span className="font-mono text-[13px] text-ink">{r.name}</span>
              </div>
              <StatusPill status={r.status || "unknown"} label={dnsStatusLabel[r.status || ""]} />
            </div>
            <div className="mt-3 flex items-center gap-1 rounded-lg border border-line bg-inset px-3 py-2">
              <code className="flex-1 select-all break-all font-mono text-[12px] leading-relaxed text-muted">
                {r.value}
              </code>
              <CopyButton text={r.value} />
            </div>
            {r.status === "mismatch" && r.found && (
              <div className="mt-2 rounded-lg border border-warn/30 bg-warn/10 px-3 py-2 font-mono text-[12px] text-warn">
                currently: {r.found}
              </div>
            )}
            <p className="mt-2.5 text-[12.5px] leading-relaxed text-faint">{r.explanation}</p>
          </Card>
        ))}
      </div>
    </div>
  );
}
