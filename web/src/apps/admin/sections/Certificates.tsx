import { RotateCw } from "lucide-react";
import { useState } from "react";
import { get, post } from "../../../lib/api";
import { useLoad } from "../../../lib/hooks";
import type { Certificate } from "../../../lib/types";
import { daysUntil, formatDate } from "../../../lib/format";
import {
  Button,
  Card,
  EmptyState,
  ErrorNote,
  Identifier,
  Spinner,
  StatusPill,
  Table,
  Td,
  Th,
  toast,
} from "../../../components/ui";

export default function Certificates() {
  const { data, error, busy, reload } = useLoad(() =>
    get<{ certificates: Certificate[] }>("/api/admin/certificates"),
  );
  const [renewingID, setRenewingID] = useState<number | null>(null);

  return (
    <div className="space-y-4">
      {error && <ErrorNote>{error}</ErrorNote>}
      {busy && !data && <Spinner />}
      {data && (
        <Card>
          {data.certificates.length === 0 ? (
            <EmptyState title="No certificates tracked" />
          ) : (
            <Table
              head={
                <>
                  <Th>Hostname</Th>
                  <Th>Status</Th>
                  <Th>Issuer</Th>
                  <Th>Expires</Th>
                  <Th>Auto-renew</Th>
                  <Th />
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
                      {c.last_error && (
                        <div className="mt-1.5 max-w-xs break-words text-[11.5px] leading-relaxed text-danger">
                          {c.last_error}
                        </div>
                      )}
                    </Td>
                    <Td className="text-muted">
                      {c.challenge_type === "http-01" ? "Let's Encrypt" : "self-signed"}
                    </Td>
                    <Td className={days !== null && days < 14 ? "text-warn" : "text-muted"}>
                      {c.not_after ? (
                        <>
                          {formatDate(c.not_after).split(",")[0]}
                          {days !== null && <span className="ml-1 text-faint">({days}d)</span>}
                        </>
                      ) : (
                        "—"
                      )}
                    </Td>
                    <Td className="text-muted">
                      {c.renew_after ? `after ${formatDate(c.renew_after).split(",")[0]}` : "—"}
                    </Td>
                    <Td className="text-right">
                      <Button
                        size="sm"
                        onClick={async () => {
                          setRenewingID(c.id);
                          try {
                            await post(`/api/admin/certificates/${c.id}/renew`);
                            toast(`Renewal started for ${c.hostname} — refresh in a moment`);
                            setTimeout(reload, 2500);
                          } catch (e: any) {
                            toast(e.message, "error");
                          } finally {
                            setRenewingID(null);
                          }
                        }}
                        busy={renewingID === c.id}
                      >
                        <RotateCw size={12} />
                        {c.status === "error" || c.status === "dns_wait" ? "Retry" : "Renew"}
                      </Button>
                    </Td>
                  </tr>
                );
              })}
            </Table>
          )}
        </Card>
      )}
    </div>
  );
}
