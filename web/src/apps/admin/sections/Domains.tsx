import { type FormEvent, useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import { del, get, post } from "../../../lib/api";
import { useLoad } from "../../../lib/hooks";
import type { Domain } from "../../../lib/types";
import {
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorNote,
  Field,
  IconButton,
  Identifier,
  Input,
  Modal,
  Spinner,
  StatusPill,
  Table,
  Td,
  Th,
  toast,
} from "../../../components/ui";

export default function Domains() {
  const { data, error, busy, reload } = useLoad(() =>
    get<{ domains: Domain[] }>("/api/admin/domains"),
  );
  const [adding, setAdding] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<Domain | null>(null);

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button variant="primary" size="sm" onClick={() => setAdding(true)}>
          <Plus size={13} /> Add domain
        </Button>
      </div>

      {error && <ErrorNote>{error}</ErrorNote>}
      {busy && !data && <Spinner />}
      {data && (
        <Card>
          {data.domains.length === 0 ? (
            <EmptyState title="No domains yet" />
          ) : (
            <Table
              head={
                <>
                  <Th>Domain</Th>
                  <Th>Mail hostname</Th>
                  <Th>DNS</Th>
                  <Th>Certificate</Th>
                  <Th>Mailboxes</Th>
                  <Th>Delivery</Th>
                  <Th />
                </>
              }
            >
              {data.domains.map((d) => (
                <tr key={d.id} className="group">
                  <Td>
                    <Identifier>{d.name}</Identifier>
                  </Td>
                  <Td>
                    <Identifier muted>{d.mail_hostname}</Identifier>
                  </Td>
                  <Td>
                    <StatusPill status={d.status} label={d.status === "active" ? "ok" : d.status} />
                  </Td>
                  <Td>
                    <StatusPill status={d.cert_status ?? "none"} />
                  </Td>
                  <Td className="tabular-nums text-muted">{d.mailbox_count}</Td>
                  <Td className="text-muted">
                    {d.delivery_mode}
                    {d.delivery_source === "domain" && (
                      <span className="ml-1 text-[10.5px] uppercase text-faint">override</span>
                    )}
                  </Td>
                  <Td className="text-right">
                    <IconButton
                      title="Delete domain"
                      tone="danger"
                      revealOnRowHover
                      onClick={() => setConfirmDelete(d)}
                      icon={<Trash2 size={14} />}
                    />
                  </Td>
                </tr>
              ))}
            </Table>
          )}
        </Card>
      )}

      {adding && <AddDomain onClose={() => setAdding(false)} onDone={reload} />}
      {confirmDelete && (
        <ConfirmDialog
          title={`Delete ${confirmDelete.name}?`}
          confirmLabel="Delete domain"
          danger
          onClose={() => setConfirmDelete(null)}
          onConfirm={async () => {
            const res = await del<{ warning?: string }>(`/api/admin/domains/${confirmDelete.id}`);
            toast(res.warning || `${confirmDelete.name} deleted`);
            reload();
          }}
        >
          This removes the domain, its mailboxes, and aliases from the mail server
          configuration. Mail stored on disk is not deleted.
        </ConfirmDialog>
      )}
    </div>
  );
}

function AddDomain({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const [name, setName] = useState("");
  const [hostname, setHostname] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const effectiveHost = hostname || (name ? `mail.${name}` : "mail.example.com");

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await post<{ warning?: string }>("/api/admin/domains", {
        name: name.trim().toLowerCase(),
        mail_hostname: hostname.trim().toLowerCase(),
      });
      toast(res.warning || `Domain added — check its DNS records next`);
      onDone();
      onClose();
    } catch (err: any) {
      setError(err.message);
      setBusy(false);
    }
  }

  return (
    <Modal title="Add domain" onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Domain">
          <Input
            autoFocus
            required
            placeholder="example.com"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Field>
        <Field
          label="Mail hostname"
          hint={`Default: ${effectiveHost}. Used for webmail, IMAP, and SMTP.`}
        >
          <Input
            placeholder={name ? `mail.${name}` : "mail.example.com"}
            value={hostname}
            onChange={(e) => setHostname(e.target.value)}
          />
        </Field>
        <ErrorNote>{error}</ErrorNote>
        <div className="flex justify-end gap-2">
          <Button type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" busy={busy}>
            Add domain
          </Button>
        </div>
      </form>
    </Modal>
  );
}
