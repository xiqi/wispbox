import { type FormEvent, useState } from "react";
import { KeyRound, Plus, Trash2 } from "lucide-react";
import { del, get, patch, post } from "../../../lib/api";
import { formatQuota } from "../../../lib/format";
import { useLoad } from "../../../lib/hooks";
import type { Domain, Mailbox } from "../../../lib/types";
import {
  Button,
  Card,
  ConfirmDialog,
  EmptyState,
  ErrorNote,
  Field,
  IconButton,
  Identifier,
  InfoNote,
  Input,
  Modal,
  Select,
  Spinner,
  Table,
  Td,
  Th,
  Toggle,
  toast,
} from "../../../components/ui";

export default function Mailboxes() {
  const domains = useLoad(() => get<{ domains: Domain[] }>("/api/admin/domains"));
  const [filter, setFilter] = useState(0);
  const boxes = useLoad(
    () => get<{ mailboxes: Mailbox[] }>(`/api/admin/mailboxes${filter ? `?domain_id=${filter}` : ""}`),
    [filter],
  );
  const [creating, setCreating] = useState(false);
  const [resetting, setResetting] = useState<Mailbox | null>(null);
  const [deleting, setDeleting] = useState<Mailbox | null>(null);

  const reload = boxes.reload;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="w-56">
          <Select value={filter} onChange={(e) => setFilter(Number(e.target.value))}>
            <option value={0}>All domains</option>
            {domains.data?.domains.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </Select>
        </div>
        <Button
          variant="primary"
          size="sm"
          onClick={() => setCreating(true)}
          disabled={!domains.data || domains.data.domains.length === 0}
        >
          <Plus size={13} /> Create mailbox
        </Button>
      </div>

      {boxes.error && <ErrorNote>{boxes.error}</ErrorNote>}
      {boxes.busy && !boxes.data && <Spinner />}
      {boxes.data && (
        <Card>
          {boxes.data.mailboxes.length === 0 ? (
            <EmptyState
              title={domains.data?.domains.length ? "No mailboxes" : "Add a domain first"}
            />
          ) : (
            <Table
              head={
                <>
                  <Th>Address</Th>
                  <Th>Status</Th>
                  <Th>Quota</Th>
                  <Th>Created</Th>
                  <Th />
                </>
              }
            >
              {boxes.data.mailboxes.map((m) => (
                <tr key={m.id} className="group">
                  <Td>
                    <Identifier>{m.email}</Identifier>
                  </Td>
                  <Td>
                    <Toggle
                      checked={m.enabled}
                      onChange={async (v) => {
                        try {
                          await patch(`/api/admin/mailboxes/${m.id}`, { enabled: v });
                          toast(v ? `${m.email} enabled` : `${m.email} disabled`);
                          reload();
                        } catch (e: any) {
                          toast(e.message, "error");
                        }
                      }}
                      label={m.enabled ? "enabled" : "disabled"}
                    />
                  </Td>
                  <Td>
                    <QuotaEditor mailbox={m} onSaved={reload} />
                  </Td>
                  <Td className="text-muted">{m.created_at?.slice(0, 10)}</Td>
                  <Td className="text-right">
                    <div className="flex items-center justify-end gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                      <IconButton
                        title="Reset password"
                        onClick={() => setResetting(m)}
                        icon={<KeyRound size={14} />}
                      />
                      <IconButton
                        title="Delete mailbox"
                        tone="danger"
                        onClick={() => setDeleting(m)}
                        icon={<Trash2 size={14} />}
                      />
                    </div>
                  </Td>
                </tr>
              ))}
            </Table>
          )}
        </Card>
      )}

      {creating && domains.data && (
        <CreateMailbox domains={domains.data.domains} onClose={() => setCreating(false)} onDone={reload} />
      )}
      {resetting && <ResetPassword mailbox={resetting} onClose={() => setResetting(null)} />}
      {deleting && (
        <ConfirmDialog
          title={`Delete ${deleting.email}?`}
          confirmLabel="Delete mailbox"
          danger
          onClose={() => setDeleting(null)}
          onConfirm={async () => {
            await del(`/api/admin/mailboxes/${deleting.id}`);
            toast(`${deleting.email} deleted`);
            reload();
          }}
        >
          The account is removed and can no longer sign in. Stored mail stays on disk until you
          remove it manually.
        </ConfirmDialog>
      )}
    </div>
  );
}

function QuotaEditor({ mailbox, onSaved }: { mailbox: Mailbox; onSaved: () => void }) {
  const [editing, setEditing] = useState(false);
  const [value, setValue] = useState(String(mailbox.quota_mb));
  if (!editing) {
    return (
      <button
        className="rounded px-1 text-[12.5px] tabular-nums text-muted underline decoration-dotted underline-offset-2 hover:text-ink"
        onClick={() => setEditing(true)}
        title="Edit quota"
      >
        {formatQuota(mailbox.quota_mb)}
      </button>
    );
  }
  return (
    <form
      className="flex items-center gap-1"
      onSubmit={async (e) => {
        e.preventDefault();
        try {
          await patch(`/api/admin/mailboxes/${mailbox.id}`, { quota_mb: Number(value) });
          toast("Quota updated");
          setEditing(false);
          onSaved();
        } catch (err: any) {
          toast(err.message, "error");
        }
      }}
    >
      <input
        autoFocus
        className="h-7 w-20 rounded-md border border-line bg-inset px-2 text-[12.5px] text-ink focus:outline-none"
        value={value}
        onChange={(e) => setValue(e.target.value.replace(/\D/g, ""))}
        onBlur={() => setEditing(false)}
      />
      <span className="text-[11px] text-faint">MB</span>
    </form>
  );
}

function CreateMailbox({
  domains,
  onClose,
  onDone,
}: {
  domains: Domain[];
  onClose: () => void;
  onDone: () => void;
}) {
  const [domainID, setDomainID] = useState(domains[0]?.id ?? 0);
  const [localPart, setLocalPart] = useState("");
  const [password, setPassword] = useState("");
  const [quota, setQuota] = useState("1024");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const domain = domains.find((d) => d.id === domainID);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await post<{ warning?: string }>("/api/admin/mailboxes", {
        domain_id: domainID,
        local_part: localPart.trim().toLowerCase(),
        password,
        quota_mb: Number(quota) || 1024,
      });
      toast(res.warning || `${localPart}@${domain?.name} created`);
      onDone();
      onClose();
    } catch (err: any) {
      setError(err.message);
      setBusy(false);
    }
  }

  return (
    <Modal title="Create mailbox" onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Address">
          <div className="flex items-center gap-2">
            <Input
              autoFocus
              required
              placeholder="alice"
              value={localPart}
              onChange={(e) => setLocalPart(e.target.value)}
              className="flex-1"
            />
            <span className="text-[13px] text-muted">@</span>
            <div className="w-44">
              <Select value={domainID} onChange={(e) => setDomainID(Number(e.target.value))}>
                {domains.map((d) => (
                  <option key={d.id} value={d.id}>
                    {d.name}
                  </option>
                ))}
              </Select>
            </div>
          </div>
        </Field>
        <Field label="Password" hint="At least 10 characters. The user can sign in to webmail and IMAP with it.">
          <Input
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </Field>
        <Field label="Quota (MB)">
          <Input value={quota} onChange={(e) => setQuota(e.target.value.replace(/\D/g, ""))} />
        </Field>
        <ErrorNote>{error}</ErrorNote>
        <div className="flex justify-end gap-2">
          <Button type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" busy={busy}>
            Create mailbox
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function ResetPassword({ mailbox, onClose }: { mailbox: Mailbox; onClose: () => void }) {
  const [password, setPassword] = useState("");
  const [generated, setGenerated] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function reset(generate: boolean) {
    setBusy(true);
    setError("");
    try {
      const res = await post<{ generated_password: string }>(
        `/api/admin/mailboxes/${mailbox.id}/reset-password`,
        generate ? { generate: true } : { password },
      );
      if (generate) {
        setGenerated(res.generated_password);
      } else {
        toast("Password updated");
        onClose();
      }
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Modal title={`Reset password — ${mailbox.email}`} onClose={onClose}>
      {generated ? (
        <div className="space-y-4">
          <InfoNote>
            New password generated. Share it with the user now — it is not shown again.
          </InfoNote>
          <div className="select-all rounded-lg border border-accent/40 bg-accent-dim px-4 py-3 text-center font-mono text-[15px] text-accent">
            {generated}
          </div>
          <div className="flex justify-end">
            <Button variant="primary" onClick={onClose}>
              Done
            </Button>
          </div>
        </div>
      ) : (
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            reset(false);
          }}
        >
          <Field label="New password">
            <Input
              type="password"
              autoFocus
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="At least 10 characters"
            />
          </Field>
          <ErrorNote>{error}</ErrorNote>
          <div className="flex items-center justify-between gap-2">
            <Button type="button" variant="ghost" busy={busy} onClick={() => reset(true)}>
              Generate one for me
            </Button>
            <div className="flex gap-2">
              <Button type="button" onClick={onClose}>
                Cancel
              </Button>
              <Button type="submit" variant="primary" busy={busy} disabled={!password}>
                Set password
              </Button>
            </div>
          </div>
        </form>
      )}
    </Modal>
  );
}
