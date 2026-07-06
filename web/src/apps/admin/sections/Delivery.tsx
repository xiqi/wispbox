import { type FormEvent, useMemo, useState } from "react";
import { CheckCircle2, FlaskConical, Plus, Route, Send, Server, Trash2, type LucideIcon } from "lucide-react";
import { del, get, patch, post } from "../../../lib/api";
import { useLoad } from "../../../lib/hooks";
import type { Domain, Policy, Relay, RelayPreset } from "../../../lib/types";
import {
  Button,
  Card,
  ConfirmDialog,
  ErrorNote,
  Field,
  IconButton,
  Identifier,
  InfoNote,
  Input,
  Modal,
  Select,
  Spinner,
  StatusPill,
  Table,
  Td,
  Th,
  toast,
} from "../../../components/ui";

export default function Delivery() {
  const domains = useLoad(() => get<{ domains: Domain[] }>("/api/admin/domains"));
  const relays = useLoad(() => get<{ relays: Relay[] }>("/api/admin/relays"));
  const policies = useLoad(() =>
    get<{ policies: Policy[]; outbound_smtp_25_open: boolean | null }>("/api/admin/delivery-policies"),
  );
  const presets = useLoad(() => get<{ presets: RelayPreset[] }>("/api/admin/relay-presets"));
  const [addingRelay, setAddingRelay] = useState(false);
  const [deletingRelay, setDeletingRelay] = useState<Relay | null>(null);
  const [testTo, setTestTo] = useState("");
  const [testBusy, setTestBusy] = useState(false);

  const reloadAll = () => {
    domains.reload();
    relays.reload();
    policies.reload();
  };

  if ((domains.busy && !domains.data) || (policies.busy && !policies.data)) return <Spinner />;

  const globalPolicy = policies.data?.policies.find((p) => p.scope_type === "global");
  const outbound25Blocked = policies.data?.outbound_smtp_25_open === false;
  const directBlockedMessage =
    "Outbound port 25 is not available on this server. Add an SMTP relay and choose relay instead.";
  const domainPolicies = new Map(
    (policies.data?.policies ?? [])
      .filter((p) => p.scope_type === "domain")
      .map((p) => [p.scope_id, p]),
  );

  async function setPolicy(scope: "global" | "domain", scopeID: number, value: string) {
    if (value === "direct" && outbound25Blocked) {
      toast(directBlockedMessage, "error");
      return;
    }
    const currentValue =
      scope === "global"
        ? policyValue(globalPolicy)
        : policyValue(domainPolicies.get(scopeID));
    if (currentValue === value) return;

    let mode = value;
    let relayID: number | null = null;
    if (value.startsWith("relay:")) {
      mode = "relay";
      relayID = Number(value.slice(6));
    }
    try {
      const res = await post<{ warning?: string }>("/api/admin/delivery-policies", {
        scope_type: scope,
        scope_id: scopeID,
        mode,
        relay_id: relayID,
      });
      toast(res.warning || "Delivery policy updated");
      reloadAll();
    } catch (e: any) {
      toast(e.message, "error");
    }
  }

  const policyValue = (p?: Policy) =>
    !p || p.mode === "inherit"
      ? "inherit"
      : p.mode === "relay"
        ? `relay:${p.relay_id}`
        : "direct";

  const relayOptions = (relays.data?.relays ?? []).filter((r) => r.enabled);

  return (
    <div className="space-y-5">
      <Card title="Global sending method">
        <div className="space-y-3">
          <div role="radiogroup" aria-label="Global sending method" className="grid min-w-0 gap-2 md:grid-cols-2">
            <MethodOption
              active={policyValue(globalPolicy) === "direct"}
              icon={Server}
              title="Direct"
              detail="This server sends mail itself."
              meta="Needs port 25 egress, PTR, and a clean IP."
              onClick={() => setPolicy("global", 0, "direct")}
            />
            {relayOptions.length === 0 ? (
              <MethodOption
                disabled
                icon={Route}
                title="Relay"
                detail="Add an SMTP relay below to use one."
                meta="SES, Postmark, Mailgun, SMTP2GO, Resend, or custom SMTP."
              />
            ) : (
              relayOptions.map((r) => (
                <MethodOption
                  key={r.id}
                  active={policyValue(globalPolicy) === `relay:${r.id}`}
                  icon={Route}
                  title={r.name}
                  detail="Send through this relay."
                  meta={`${r.host}:${r.port} · ${formatTLSMode(r.tls_mode)}`}
                  onClick={() => setPolicy("global", 0, `relay:${r.id}`)}
                />
              ))
            )}
          </div>
          <p className="text-[12.5px] leading-relaxed text-faint">
            Relay delivery stays explicit; wispbox never silently falls back to direct sending.
          </p>
          {outbound25Blocked && <ErrorNote>{directBlockedMessage}</ErrorNote>}
        </div>
      </Card>

      <Card
        title="Per-domain overrides"
      >
        {domains.data && domains.data.domains.length === 0 ? (
          <div className="py-3 text-center text-[13px] text-faint">Add a domain first.</div>
        ) : (
          <Table
            head={
              <>
                <Th>Domain</Th>
                <Th>Sending method</Th>
                <Th>Effective</Th>
              </>
            }
          >
            {domains.data?.domains.map((d) => {
              const p = domainPolicies.get(d.id);
              return (
                <tr key={d.id}>
                  <Td>
                    <Identifier>{d.name}</Identifier>
                  </Td>
                  <Td>
                    <div className="w-64">
                      <Select
                        value={policyValue(p)}
                        onChange={(e) => setPolicy("domain", d.id, e.target.value)}
                      >
                        <option value="inherit">Inherit global</option>
                        <option value="direct">Direct</option>
                        {relayOptions.map((r) => (
                          <option key={r.id} value={`relay:${r.id}`}>
                            Relay through {r.name}
                          </option>
                        ))}
                      </Select>
                    </div>
                  </Td>
                  <Td className="text-muted">{formatDeliveryMode(d.delivery_mode)}</Td>
                </tr>
              );
            })}
          </Table>
        )}
      </Card>

      <Card
        title="SMTP relays"
        actions={
          <Button size="sm" variant="primary" onClick={() => setAddingRelay(true)}>
            <Plus size={13} /> Add relay
          </Button>
        }
      >
        {relays.data && relays.data.relays.length === 0 ? (
          <div className="py-3 text-center text-[13px] text-faint">
            No relays configured.
          </div>
        ) : (
          <Table
            head={
              <>
                <Th>Name</Th>
                <Th>Provider</Th>
                <Th>Endpoint</Th>
                <Th>TLS</Th>
                <Th>Status</Th>
                <Th />
              </>
            }
          >
            {relays.data?.relays.map((r) => (
              <tr key={r.id} className="group">
                <Td className="font-medium text-ink">{r.name}</Td>
                <Td className="text-muted">{r.provider}</Td>
                <Td>
                  <Identifier muted>{r.host}:{r.port}</Identifier>
                </Td>
                <Td className="text-muted">{formatTLSMode(r.tls_mode)}</Td>
                <Td>
                  <StatusPill status={r.enabled ? "ok" : "inactive"} />
                </Td>
                <Td className="text-right">
                  <div className="flex items-center justify-end gap-1">
                    <IconButton
                      title="Test connection & authentication"
                      tone="accent"
                      icon={<FlaskConical size={14} />}
                      onClick={async () => {
                        toast(`Testing ${r.name}…`);
                        try {
                          const res = await post<{ ok: boolean; error?: string }>(
                            `/api/admin/relays/${r.id}/test`,
                          );
                          if (res.ok) toast(`${r.name}: connection and login OK`);
                          else toast(`${r.name}: ${res.error}`, "error");
                        } catch (e: any) {
                          toast(e.message, "error");
                        }
                      }}
                    />
                    <IconButton
                      title="Delete relay"
                      tone="danger"
                      revealOnRowHover
                      onClick={() => setDeletingRelay(r)}
                      icon={<Trash2 size={14} />}
                    />
                  </div>
                </Td>
              </tr>
            ))}
          </Table>
        )}
      </Card>

      <Card title="Send a test email">
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={async (e: FormEvent) => {
            e.preventDefault();
            setTestBusy(true);
            try {
              const res = await post<{ ok: boolean; error?: string }>("/api/admin/test-email", {
                to: testTo,
              });
              if (res.ok) toast(`Test email queued for ${testTo}`);
              else toast(res.error ?? "test failed", "error");
            } catch (err: any) {
              toast(err.message, "error");
            } finally {
              setTestBusy(false);
            }
          }}
        >
          <div className="w-72">
            <Input
              type="email"
              required
              placeholder="you@somewhere-else.com"
              value={testTo}
              onChange={(e) => setTestTo(e.target.value)}
            />
          </div>
          <Button type="submit" variant="primary" busy={testBusy}>
            <Send size={13} /> Send test
          </Button>
        </form>
      </Card>

      {addingRelay && presets.data && (
        <AddRelay
          presets={presets.data.presets}
          onClose={() => setAddingRelay(false)}
          onDone={reloadAll}
        />
      )}
      {deletingRelay && (
        <ConfirmDialog
          title={`Delete relay ${deletingRelay.name}?`}
          confirmLabel="Delete relay"
          danger
          onClose={() => setDeletingRelay(null)}
          onConfirm={async () => {
            await del(`/api/admin/relays/${deletingRelay.id}`);
            toast("Relay deleted");
            reloadAll();
          }}
        >
          Any policy using this relay must be changed before mail can send through it again.
        </ConfirmDialog>
      )}
    </div>
  );
}

function formatDeliveryMode(mode?: string): string {
  if (mode === "relay") return "Relay";
  if (mode === "direct") return "Direct";
  if (mode === "inherit") return "Inherit";
  return mode ? mode[0].toUpperCase() + mode.slice(1) : "—";
}

function formatTLSMode(mode: Relay["tls_mode"]): string {
  return mode === "starttls" ? "STARTTLS" : "Implicit TLS";
}

function MethodOption({
  active,
  disabled,
  icon: Icon,
  title,
  detail,
  meta,
  onClick,
}: {
  active?: boolean;
  disabled?: boolean;
  icon: LucideIcon;
  title: string;
  detail: string;
  meta?: string;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={Boolean(active)}
      disabled={disabled}
      onClick={onClick}
      className={`flex min-h-[5.5rem] w-full min-w-0 items-start gap-3 rounded-lg border p-3 text-left transition-colors disabled:pointer-events-none disabled:opacity-55 ${
        active
          ? "border-accent/60 bg-accent-dim text-ink"
          : "border-line bg-inset/40 text-muted hover:border-line-strong hover:bg-inset"
      }`}
    >
      <span
        className={`mt-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border ${
          active ? "border-accent/50 bg-accent text-accent-ink" : "border-line bg-raised text-muted"
        }`}
      >
        <Icon size={15} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2">
          <span className={`truncate text-[13.5px] font-semibold ${active ? "text-ink" : "text-muted"}`}>
            {title}
          </span>
          {active && <CheckCircle2 size={14} className="shrink-0 text-accent" />}
        </span>
        <span className="mt-1 block text-[12.5px] leading-relaxed text-muted">{detail}</span>
        {meta && <span className="mt-1 block truncate font-mono text-[11.5px] text-faint">{meta}</span>}
      </span>
    </button>
  );
}

function AddRelay({
  presets,
  onClose,
  onDone,
}: {
  presets: RelayPreset[];
  onClose: () => void;
  onDone: () => void;
}) {
  const [provider, setProvider] = useState(presets[0]?.provider ?? "custom");
  const preset = useMemo(
    () => presets.find((p) => p.provider === provider) ?? presets[presets.length - 1],
    [presets, provider],
  );
  const [name, setName] = useState("");
  const [host, setHost] = useState(preset?.host ?? "");
  const [port, setPort] = useState(String(preset?.port ?? 587));
  const [tlsMode, setTlsMode] = useState<string>(preset?.tls_mode ?? "starttls");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  function pickProvider(p: string) {
    setProvider(p);
    const pr = presets.find((x) => x.provider === p);
    if (pr) {
      setHost(pr.host);
      setPort(String(pr.port));
      setTlsMode(pr.tls_mode);
      if (!name || presets.some((x) => x.label === name)) setName(pr.label);
    }
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await post<{ warning?: string }>("/api/admin/relays", {
        name: name || preset?.label || provider,
        provider,
        host,
        port: Number(port),
        username,
        password,
        tls_mode: tlsMode,
      });
      toast(res.warning || "Relay saved — select it in a delivery policy to use it");
      onDone();
      onClose();
    } catch (err: any) {
      setError(err.message);
      setBusy(false);
    }
  }

  return (
    <Modal title="Add SMTP relay" onClose={onClose} wide>
      <form onSubmit={submit} className="space-y-4">
        <Field label="Provider">
          <div className="grid grid-cols-3 gap-2">
            {presets.map((p) => (
              <button
                key={p.provider}
                type="button"
                onClick={() => pickProvider(p.provider)}
                className={`rounded-lg border px-3 py-2 text-[12.5px] font-medium transition-colors ${
                  provider === p.provider
                    ? "border-accent/60 bg-accent-dim text-accent"
                    : "border-line text-muted hover:border-line-strong hover:text-ink"
                }`}
              >
                {p.label}
              </button>
            ))}
          </div>
        </Field>
        {preset?.note && <InfoNote>{preset.note}</InfoNote>}
        <div className="grid gap-4 sm:grid-cols-2">
          <Field label="Name">
            <Input value={name} placeholder={preset?.label} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="TLS">
            <Select value={tlsMode} onChange={(e) => setTlsMode(e.target.value)}>
              <option value="starttls">STARTTLS (usually port 587)</option>
              <option value="tls">Implicit TLS (usually port 465)</option>
            </Select>
          </Field>
          <Field label="Host">
            <Input required value={host} onChange={(e) => setHost(e.target.value)} />
          </Field>
          <Field label="Port">
            <Input required value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))} />
          </Field>
          <Field label="Username" hint={preset?.username_hint}>
            <Input value={username} onChange={(e) => setUsername(e.target.value)} />
          </Field>
          <Field label="Password">
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
          </Field>
        </div>
        <ErrorNote>{error}</ErrorNote>
        <div className="flex justify-end gap-2">
          <Button type="button" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant="primary" busy={busy}>
            Save relay
          </Button>
        </div>
      </form>
    </Modal>
  );
}
