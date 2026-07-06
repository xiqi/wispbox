import { useState } from "react";
import { get } from "../../../lib/api";
import { useLoad } from "../../../lib/hooks";
import type { LogLine } from "../../../lib/types";
import { EmptyState, ErrorNote, RefreshButton, Spinner } from "../../../components/ui";

const tabs = [
  { id: "", label: "All" },
  { id: "postfix", label: "Postfix" },
  { id: "dovecot", label: "Dovecot" },
  { id: "wispboxd", label: "Daemon" },
];

export default function Logs() {
  const [service, setService] = useState("");
  const [refreshing, setRefreshing] = useState(false);
  const { data, error, busy, reload } = useLoad(
    () => get<{ lines: LogLine[] }>(`/api/admin/logs?n=200${service ? `&service=${service}` : ""}`),
    [service],
  );
  const refreshBusy = refreshing || (busy && Boolean(data));

  function refreshLogs() {
    if (refreshing) return;
    setRefreshing(true);
    reload();
    window.setTimeout(() => setRefreshing(false), 500);
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between gap-3">
        <div className="flex gap-1 rounded-lg border border-line bg-inset p-1">
          {tabs.map((t) => (
            <button
              key={t.id}
              onClick={() => setService(t.id)}
              className={`rounded-md px-3 py-1 text-[12.5px] font-medium transition-colors ${
                service === t.id ? "bg-raised text-ink shadow-soft" : "text-muted hover:text-ink"
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
        <RefreshButton size="sm" onClick={refreshLogs} busy={refreshBusy} />
      </div>

      {error && <ErrorNote>{error}</ErrorNote>}
      {busy && !data && <Spinner />}
      {data &&
        (data.lines.length === 0 ? (
          <EmptyState title="No log lines" />
        ) : (
          <div className="overflow-x-auto rounded-xl border border-line bg-bg-deep p-4 font-mono text-[12px] leading-[1.7]">
            {data.lines.map((l, i) => (
              <div key={i} className="whitespace-pre-wrap break-all">
                <span className="text-faint">{l.time?.replace("T", " ").replace("Z", "")} </span>
                <span
                  className={
                    l.service === "postfix"
                      ? "text-info"
                      : l.service === "dovecot"
                        ? "text-warn"
                        : "text-accent"
                  }
                >
                  {l.service}
                </span>
                <span className="text-muted"> {l.message}</span>
              </div>
            ))}
          </div>
        ))}
    </div>
  );
}
