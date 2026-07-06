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
      ? "Issue certificate"
      : adminCert.status === "error" || adminCert.status === "dns_wait"
        ? "Retry certificate"
        : "Renew certificate";

  async function issueAdminCertificate() {
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
      toast(`Certificate request started for ${res.certificate.hostname}`);
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
            title="Admin certificate"
            actions={
              <Button
                size="sm"
                variant="primary"
                onClick={issueAdminCertificate}
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
                <div className="text-[12px] font-medium text-faint">Hostname</div>
                <Identifier className="mt-2 block">{data.primary_hostname || "not set"}</Identifier>
              </div>
              <div>
                <div className="text-[12px] font-medium text-faint">Status</div>
                <div className="mt-2">
                  {adminCert ? <StatusPill status={adminCert.status} /> : <StatusPill status="none" />}
                </div>
              </div>
              <div>
                <div className="text-[12px] font-medium text-faint">Expires</div>
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

          <Card title="Tracked certificates">
            {data.certificates.length === 0 ? (
              <EmptyState title="No certificates tracked" />
            ) : (
              <>
                <div className="divide-y divide-line md:hidden">
                  {data.certificates.map((c) => {
                    const days = daysUntil(c.not_after);
                    return (
                      <div key={c.id} className="space-y-3 py-3 first:pt-0 last:pb-0">
                        <div className="flex flex-wrap items-center justify-between gap-2">
                          <Identifier>{c.hostname}</Identifier>
                          <StatusPill status={c.status} />
                        </div>
                        {c.last_error && (
                          <div className="break-words text-[11.5px] leading-relaxed text-danger">
                            {c.last_error}
                          </div>
                        )}
                        <div className="grid grid-cols-2 gap-3 text-[12.5px]">
                          <CertFact label="Issuer" value={issuerLabel(c)} />
                          <CertFact
                            label="Expires"
                            value={c.not_after ? `${formatDate(c.not_after).split(",")[0]}${days !== null ? ` (${days}d)` : ""}` : "—"}
                            tone={days !== null && days < 14 ? "warn" : undefined}
                          />
                          <CertFact
                            label="Renews after"
                            value={c.renew_after ? formatDate(c.renew_after).split(",")[0] : "—"}
                          />
                          <div className="flex items-end justify-start">
                            <RenewButton
                              certificate={c}
                              busy={renewingID === c.id}
                              onStart={() => setRenewingID(c.id)}
                              onDone={() => setRenewingID(null)}
                              reload={reload}
                            />
                          </div>
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
                        <Th>Issuer</Th>
                        <Th>Expires</Th>
                        <Th>Renews after</Th>
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
                          <Td className="text-muted">{issuerLabel(c)}</Td>
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
                            {c.renew_after ? formatDate(c.renew_after).split(",")[0] : "—"}
                          </Td>
                          <Td className="text-right">
                            <RenewButton
                              certificate={c}
                              busy={renewingID === c.id}
                              onStart={() => setRenewingID(c.id)}
                              onDone={() => setRenewingID(null)}
                              reload={reload}
                            />
                          </Td>
                        </tr>
                      );
                    })}
                  </Table>
                </div>
              </>
            )}
          </Card>
        </>
      )}
    </div>
  );
}

function issuerLabel(c: Certificate): string {
  return c.challenge_type === "http-01" ? "Let's Encrypt" : "Self-signed";
}

function CertFact({ label, value, tone }: { label: string; value: string; tone?: "warn" }) {
  return (
    <div>
      <div className="text-[12px] font-medium text-faint">{label}</div>
      <div className={`mt-1 leading-snug ${tone === "warn" ? "text-warn" : "text-muted"}`}>{value}</div>
    </div>
  );
}

function RenewButton({
  certificate,
  busy,
  onStart,
  onDone,
  reload,
}: {
  certificate: Certificate;
  busy: boolean;
  onStart: () => void;
  onDone: () => void;
  reload: () => void;
}) {
  return (
    <Button
      size="sm"
      onClick={async () => {
        onStart();
        try {
          await post(`/api/admin/certificates/${certificate.id}/renew`);
          toast(`Renewal started for ${certificate.hostname}`);
          setTimeout(reload, 2500);
        } catch (e: any) {
          toast(e.message, "error");
        } finally {
          onDone();
        }
      }}
      busy={busy}
    >
      <RotateCw size={12} />
      {certificate.status === "error" || certificate.status === "dns_wait" ? "Retry" : "Renew"}
    </Button>
  );
}
