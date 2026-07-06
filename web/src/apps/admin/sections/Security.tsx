import { get } from "../../../lib/api";
import { useLoad } from "../../../lib/hooks";
import type { AuditEntry } from "../../../lib/types";
import { formatDate } from "../../../lib/format";
import { Card, EmptyState, ErrorNote, Spinner, Table, Td, Th } from "../../../components/ui";
import AccountSecurity from "../../../components/AccountSecurity";

export default function Security() {
  const { data, error, busy } = useLoad(() => get<{ entries: AuditEntry[] }>("/api/admin/audit"));

  return (
    <div className="space-y-5">
      <AccountSecurity base="/api/admin" />
      <Card title="Audit log">
        {error && <ErrorNote>{error}</ErrorNote>}
        {busy && !data && <Spinner />}
        {data &&
          (data.entries.length === 0 ? (
            <EmptyState title="No audit entries yet" />
          ) : (
            <Table
              head={
                <>
                  <Th>When</Th>
                  <Th>Actor</Th>
                  <Th>Action</Th>
                  <Th>Target</Th>
                  <Th>IP</Th>
                </>
              }
            >
              {data.entries.map((e) => (
                <tr key={e.id}>
                  <Td className="whitespace-nowrap text-muted">{formatDate(e.created_at)}</Td>
                  <Td className="text-muted">
                    {e.actor_type}
                    {e.actor_id > 0 && ` #${e.actor_id}`}
                  </Td>
                  <Td>
                    <span
                      className={`font-mono text-[12px] ${
                        e.action.includes("failed") || e.action.includes("delete")
                          ? "text-danger"
                          : "text-ink"
                      }`}
                    >
                      {e.action}
                    </span>
                  </Td>
                  <Td className="font-mono text-[12px] text-muted">
                    {e.target_type && `${e.target_type}: `}
                    {e.target_id}
                  </Td>
                  <Td className="font-mono text-[12px] text-faint">{e.ip}</Td>
                </tr>
              ))}
            </Table>
          ))}
      </Card>
    </div>
  );
}
