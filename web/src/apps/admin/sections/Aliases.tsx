import { type FormEvent, useState } from "react";
import { ArrowRight, Plus, Trash2 } from "lucide-react";
import { del, get, patch, post } from "../../../lib/api";
import { useLoad } from "../../../lib/hooks";
import type { Alias, Domain } from "../../../lib/types";
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
  Select,
  Spinner,
  Table,
  Td,
  Th,
  Toggle,
  toast,
} from "../../../components/ui";

export default function Aliases() {
  const domains = useLoad(() => get<{ domains: Domain[] }>("/api/admin/domains"));
  const aliases = useLoad(() => get<{ aliases: Alias[] }>("/api/admin/aliases"));
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<Alias | null>(null);

  return (
    <div className="space-y-4">
      <div className="flex justify-end">
        <Button
          variant="primary"
          size="sm"
          onClick={() => setCreating(true)}
          disabled={!domains.data || domains.data.domains.length === 0}
        >
          <Plus size={13} /> Create alias
        </Button>
      </div>

      {aliases.error && <ErrorNote>{aliases.error}</ErrorNote>}
      {aliases.busy && !aliases.data && <Spinner />}
      {aliases.data && (
        <Card>
          {aliases.data.aliases.length === 0 ? (
            <EmptyState
              title={domains.data?.domains.length ? "No aliases yet" : "Add a domain first"}
            />
          ) : (
            <Table
              head={
                <>
                  <Th>Source</Th>
                  <Th />
                  <Th>Destination</Th>
                  <Th>Type</Th>
                  <Th>Status</Th>
                  <Th />
                </>
              }
            >
              {aliases.data.aliases.map((a) => (
                <tr key={a.id} className="group">
                  <Td>
                    <Identifier>{a.source}</Identifier>
                  </Td>
                  <Td>
                    <ArrowRight size={13} className="text-faint" />
                  </Td>
                  <Td>
                    <Identifier muted>{a.destination}</Identifier>
                  </Td>
                  <Td className="text-muted">{a.is_catch_all ? "Catch-all" : "Alias"}</Td>
                  <Td>
                    <Toggle
                      checked={a.enabled}
                      onChange={async (v) => {
                        try {
                          await patch(`/api/admin/aliases/${a.id}`, { enabled: v });
                          aliases.reload();
                        } catch (e: any) {
                          toast(e.message, "error");
                        }
                      }}
                      label={a.enabled ? "Enabled" : "Disabled"}
                    />
                  </Td>
                  <Td className="text-right">
                    <IconButton
                      title="Delete alias"
                      tone="danger"
                      revealOnRowHover
                      onClick={() => setDeleting(a)}
                      icon={<Trash2 size={14} />}
                    />
                  </Td>
                </tr>
              ))}
            </Table>
          )}
        </Card>
      )}

      {creating && domains.data && (
        <CreateAlias
          domains={domains.data.domains}
          onClose={() => setCreating(false)}
          onDone={aliases.reload}
        />
      )}
      {deleting && (
        <ConfirmDialog
          title={`Delete alias ${deleting.source}?`}
          confirmLabel="Delete alias"
          danger
          onClose={() => setDeleting(null)}
          onConfirm={async () => {
            await del(`/api/admin/aliases/${deleting.id}`);
            toast("Alias deleted");
            aliases.reload();
          }}
        >
          Mail sent to {deleting.source} will no longer be forwarded to {deleting.destination}.
        </ConfirmDialog>
      )}
    </div>
  );
}

function CreateAlias({
  domains,
  onClose,
  onDone,
}: {
  domains: Domain[];
  onClose: () => void;
  onDone: () => void;
}) {
  const [domainID, setDomainID] = useState(domains[0]?.id ?? 0);
  const [source, setSource] = useState("");
  const [destination, setDestination] = useState("");
  const [catchAll, setCatchAll] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const domain = domains.find((d) => d.id === domainID);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await post<{ warning?: string }>("/api/admin/aliases", {
        domain_id: domainID,
        source: source.trim().toLowerCase(),
        destination: destination.trim().toLowerCase(),
        is_catch_all: catchAll,
      });
      toast(res.warning || "Alias created");
      onDone();
      onClose();
    } catch (err: any) {
      setError(err.message);
      setBusy(false);
    }
  }

  return (
    <Modal title="Create alias" onClose={onClose}>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Domain">
          <Select value={domainID} onChange={(e) => setDomainID(Number(e.target.value))}>
            {domains.map((d) => (
              <option key={d.id} value={d.id}>
                {d.name}
              </option>
            ))}
          </Select>
        </Field>
        <Toggle
          checked={catchAll}
          onChange={setCatchAll}
          label="Catch-all"
        />
        {!catchAll && (
          <Field label="Source">
            <div className="flex items-center gap-2">
              <Input
                autoFocus
                required
                placeholder="hello"
                value={source}
                onChange={(e) => setSource(e.target.value)}
                className="flex-1"
              />
              <span className="whitespace-nowrap text-[13px] text-muted">@{domain?.name}</span>
            </div>
          </Field>
        )}
        <Field label="Destination">
          <Input
            required
            type="email"
            placeholder="you@example.com"
            value={destination}
            onChange={(e) => setDestination(e.target.value)}
          />
        </Field>
        <ErrorNote>{error}</ErrorNote>
        <div className="flex justify-end gap-2">
          <Button type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" busy={busy}>
            Create alias
          </Button>
        </div>
      </form>
    </Modal>
  );
}
