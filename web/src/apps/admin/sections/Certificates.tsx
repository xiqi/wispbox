import { RotateCw, ShieldCheck } from "lucide-react";
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
  const { data, error, busy, reload, setData } = useLoad(() =>
    get<{
      certificates: Certificate[];
      primary_hostname: string;
      admin_certificate: Certificate | null;
    }>("/api/admin/certificates"),
  );
  const [renewingID, setRenewingID] = useState<number | null>(null);
  const [issuingAdmin, setIssuingAdmin] = useState(false);
  const adminCert =
    data?.admin_certificate ??
    data?.certificates.find((c) => c.hostname === data.primary_hostname) ??
    null;
  const adminLabel =
    !adminCert || adminCert.status === "pending"
      ? "Issue SSL"
      : adminCert.status === "error" || adminCert.status === "dns_wait"
        ? "Retry SSL"
        : "Renew SSL";

  async function issueAdminSSL() {
    setIssuingAdmin(true);
    try {
      const res = await post<{ certificate: Certificate }>("/api/admin/certificates/admin/issue");
      setData((current) =>
        current
          ? {
              ...current,
              admin_certificate: res.certificate,
              certificates: current.certificates.some((c) => c.id === res.certificate.id)
                ? current.certificates.map((c) => (c.id === res.certificate.id ? res.certificate : c))
                : [...current.certificates, res.certificate].sort((a, b) => a.hostname.localeCompare(b.hostname)),
            }
          : current,
      );
      toast(`SSL request started for ${res.certificate.hostname}`);
      setTimeout(reload, 2500);
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setIssuingAdmin(false);
    }
  }

  return (
    <div className="space-y-4">
      {error && <ErrorNote>{error}</ErrorNote>}
      {busy && !data && <Spinner />}
      {data && (
        <>
          <Card
            title="Admin SSL"
            actions={
              <Button
                size="sm"
                variant="primary"
                onClick={issueAdminSSL}
                busy={issuingAdmin}
                disabled={!data.primary_hostname}
              >
                <ShieldCheck size={13} />
                {adminLabel}
              </Button>
            }
          >
            <div className="grid gap-4 sm:grid-cols-3">
              <div>
                <div className="text-[12px] text-faint">Hostname</div>
                <Identifier className="mt-2 block">{data.primary_hostname || "not set"}</Identifier>
              </div>
              <div>
                <div className="text-[12px] text-faint">Status</div>
                <div className="mt-2">
                  {adminCert ? <StatusPill status={adminCert.status} /> : <StatusPill status="unknown" label="not issued" />}
                </div>
              </div>
              <div>
                <div className="text-[12px] text-faint">Expires</div>
                <div className="mt-2 text-[13px] text-muted">
                  {adminCert?.not_after ? formatDate(adminCert.not_after).split(",")[0] : "—"}
                </div>
              </div>
            </div>
            {adminCert?.last_error && (
              <div className="mt-3 break-words text-[12px] leading-relaxed text-danger">
                {adminCert.last_error}
              </div>
            )}
          </Card>

          <Card title="Certificates">
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
                              toast(`Renewal started for ${c.hostname}`);
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
        </>
      )}
    </div>
  );
}
