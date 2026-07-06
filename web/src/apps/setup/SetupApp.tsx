import { type FormEvent, useEffect, useMemo, useState } from "react";
import {
  ArrowRight,
  Check,
  CheckCircle2,
  Circle,
  Loader2,
  Mail,
  TriangleAlert,
  XCircle,
} from "lucide-react";
import { get, post, setCsrf } from "../../lib/api";
import { useBrand } from "../../lib/brand";
import { dnsStatusLabel } from "../../lib/format";
import { useLoad } from "../../lib/hooks";
import type { Certificate, DnsRecord, Domain, RelayPreset, SetupStatus } from "../../lib/types";
import {
  BrandMark,
  Button,
  CopyButton,
  ErrorNote,
  Field,
  Identifier,
  InfoNote,
  Input,
  RefreshButton,
  Select,
  Spinner,
  StatusPill,
  ThemeToggle,
  toast,
} from "../../components/ui";

const stepTitles = [
  "System check",
  "Admin account",
  "Server hostname",
  "First domain",
  "Sending method",
  "DNS records",
  "Certificate",
  "First mailbox",
  "Test email",
  "Done",
];

export default function SetupApp() {
  const brand = useBrand();
  const [status, setStatus] = useState<SetupStatus | null>(null);
  const [step, setStep] = useState(0);
  const [domain, setDomain] = useState<Domain | null>(null);
  const [loadError, setLoadError] = useState("");
  const [checking, setChecking] = useState(false);

  useEffect(() => {
    document.title = `${brand.name} Setup`;
  }, [brand.name]);

  async function loadStatus(resume = true) {
    setChecking(true);
    try {
      const s = await get<SetupStatus & { csrf?: string }>("/api/setup/status");
      if (s.csrf) setCsrf(s.csrf);
      setStatus(s);
      if (!resume) return;
      if (s.has_admin && !s.authenticated) setStep(1);
      else if (s.domains && s.domains.length > 0 && s.authenticated) {
        setDomain(s.domains[0]);
        setStep(s.mailbox_count > 0 ? 8 : 5);
      } else if (s.authenticated && s.primary_hostname) setStep(3);
      else if (s.has_admin && s.authenticated) setStep(2);
    } catch (e: any) {
      setLoadError(e.message);
    } finally {
      setChecking(false);
    }
  }

  useEffect(() => {
    loadStatus();
  }, []);

  if (loadError) {
    return (
      <Shell>
        <ErrorNote>{loadError} — if setup already finished, head to /admin.</ErrorNote>
      </Shell>
    );
  }
  if (!status) {
    return (
      <Shell>
        <Spinner label="Checking your server…" />
      </Shell>
    );
  }

  const next = () => setStep((s) => s + 1);

  return (
    <Shell step={step}>
      {step === 0 && (
        <StepChecks
          status={status}
          checking={checking}
          onRetry={() => loadStatus(false)}
          onNext={next}
        />
      )}
      {step === 1 && (
        <StepAdmin
          hasAdmin={status.has_admin}
          authenticated={status.authenticated}
          onNext={next}
        />
      )}
      {step === 2 && <StepHost onNext={next} />}
      {step === 3 && (
        <StepDomain
          onNext={(d) => {
            setDomain(d);
            next();
          }}
        />
      )}
      {step === 4 && <StepDelivery outbound25Open={status.outbound_smtp_25_open} onNext={next} />}
      {step === 5 && domain && <StepDns domain={domain} onNext={next} />}
      {step === 6 && domain && <StepCertificate domain={domain} onNext={next} />}
      {step === 7 && domain && <StepMailbox domain={domain} onNext={next} />}
      {step === 8 && <StepTestEmail onNext={next} />}
      {step === 9 && <StepDone />}
    </Shell>
  );
}

function Shell({ children, step }: { children: React.ReactNode; step?: number }) {
  const brand = useBrand();
  return (
    <div className="wisp-atmosphere min-h-full">
      <div className="absolute right-4 top-4 z-10">
        <ThemeToggle />
      </div>
      <div className="mx-auto flex min-h-screen max-w-5xl gap-10 px-6 py-10">
        <aside className="hidden w-56 shrink-0 md:block">
          <div className="sticky top-10">
            <div className="mb-8 flex items-center gap-2.5">
              <BrandMark size={28} />
              <div className="min-w-0 truncate text-[16px] font-semibold text-ink">{brand.name}</div>
            </div>
            {step !== undefined && (
              <ol className="space-y-1">
                {stepTitles.map((t, i) => (
                  <li
                    key={t}
                    className={`flex items-center gap-2.5 rounded-lg px-2 py-1.5 text-[13px] ${
                      i === step
                        ? "bg-accent-dim font-medium text-accent"
                        : i < step
                          ? "text-muted"
                          : "text-faint"
                    }`}
                  >
                    {i < step ? (
                      <CheckCircle2 size={14} className="text-ok" />
                    ) : i === step ? (
                      <Circle size={14} className="animate-wisp-pulse text-accent" />
                    ) : (
                      <Circle size={14} />
                    )}
                    {t}
                  </li>
                ))}
              </ol>
            )}
          </div>
        </aside>
        <main className="min-w-0 flex-1 pt-2">
          <div className="animate-rise mx-auto w-full max-w-xl">{children}</div>
        </main>
      </div>
    </div>
  );
}

function StepCard({
  title,
  lead,
  children,
}: {
  title: string;
  lead?: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <h1 className="text-[24px] font-semibold text-ink">{title}</h1>
      {lead && <p className="mt-2 text-[14px] leading-relaxed text-muted">{lead}</p>}
      <div className="mt-6 rounded-xl border border-line bg-raised/80 p-6 shadow-soft backdrop-blur">
        {children}
      </div>
    </div>
  );
}

/* step 1: system check */
function StepChecks({
  status,
  checking,
  onRetry,
  onNext,
}: {
  status: SetupStatus;
  checking: boolean;
  onRetry: () => void;
  onNext: () => void;
}) {
  const allOk = status.checks.every((c) => c.ok || !c.required);
  return (
    <StepCard title="System check">
      <ul className="space-y-3">
        {status.checks.map((c) => (
          <li key={c.name} className="flex items-start gap-3">
            {c.ok ? (
              <Check size={16} className="mt-0.5 text-ok" />
            ) : !c.required ? (
              <TriangleAlert size={16} className="mt-0.5 text-warn" />
            ) : (
              <XCircle size={16} className="mt-0.5 text-danger" />
            )}
            <div>
              <div className="flex items-center gap-2 text-[13.5px] font-medium text-ink">
                {c.name}
                {!c.required && <span className="text-[11px] font-medium text-faint">advisory</span>}
              </div>
              <div className="text-[12.5px] text-faint">{c.detail}</div>
            </div>
          </li>
        ))}
      </ul>
      <div className="mt-6 flex justify-end gap-2">
        <RefreshButton type="button" onClick={onRetry} busy={checking}>
          Retry checks
        </RefreshButton>
        <Button variant="primary" onClick={onNext} disabled={!allOk}>
          {allOk ? "Begin setup" : "Fix the issues above first"} <ArrowRight size={14} />
        </Button>
      </div>
    </StepCard>
  );
}

/* step 2: admin account */
function StepAdmin({
  hasAdmin,
  authenticated,
  onNext,
}: {
  hasAdmin: boolean;
  authenticated: boolean;
  onNext: () => void;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  if (hasAdmin && !authenticated) {
    return (
      <StepCard title="Resume setup">
        <InfoNote>
          Sign in at <a href="/admin" className="text-accent underline">/admin</a> with your admin
          account, then return to <code>/setup</code> to continue where you left off.
        </InfoNote>
      </StepCard>
    );
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await post<{ csrf: string }>("/api/setup/admin", { username, password });
      setCsrf(res.csrf);
      onNext();
    } catch (err: any) {
      setError(err.message);
      setBusy(false);
    }
  }

  return (
    <StepCard
      title="Create your admin account"
      lead="Separate from mailbox accounts."
    >
      <form onSubmit={submit} className="space-y-4">
        <Field label="Admin username" hint="Use this to sign in at /admin.">
          <Input
            type="text"
            autoFocus
            required
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </Field>
        <Field label="Password" hint="At least 10 characters.">
          <Input type="password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <ErrorNote>{error}</ErrorNote>
        <div className="flex justify-end">
          <Button type="submit" variant="primary" busy={busy}>
            Create account <ArrowRight size={14} />
          </Button>
        </div>
      </form>
    </StepCard>
  );
}

/* step 3: hostname */
function StepHost({ onNext }: { onNext: () => void }) {
  const [hostname, setHostname] = useState("");
  const [ipv4, setIpv4] = useState("");
  const [acmeEmail, setAcmeEmail] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await post("/api/setup/host", {
        hostname: hostname.trim().toLowerCase(),
        server_ipv4: ipv4.trim(),
        acme_email: acmeEmail.trim(),
      });
      onNext();
    } catch (err: any) {
      setError(err.message);
      setBusy(false);
    }
  }

  return (
    <StepCard
      title="Name your server"
      lead="Used for webmail, IMAP, and SMTP."
    >
      <form onSubmit={submit} className="space-y-4">
        <Field label="Primary hostname">
          <Input autoFocus required placeholder="mail.example.com" value={hostname} onChange={(e) => setHostname(e.target.value)} />
        </Field>
        <Field label="This server's public IPv4" hint="Used for DNS checks and certificates. Find it with: curl -4 ifconfig.me">
          <Input required placeholder="203.0.113.10" value={ipv4} onChange={(e) => setIpv4(e.target.value)} />
        </Field>
        <ReverseDnsGuide hostname={hostname} ipv4={ipv4} />
        <Field label="Certificate contact email" hint="Receives Let's Encrypt expiry warnings.">
          <Input type="email" placeholder="you@somewhere.com" value={acmeEmail} onChange={(e) => setAcmeEmail(e.target.value)} />
        </Field>
        <ErrorNote>{error}</ErrorNote>
        <div className="flex justify-end">
          <Button type="submit" variant="primary" busy={busy}>
            Continue <ArrowRight size={14} />
          </Button>
        </div>
      </form>
    </StepCard>
  );
}

function ReverseDnsGuide({ hostname, ipv4 }: { hostname: string; ipv4: string }) {
  const ptrHost = hostname.trim().toLowerCase() || "mail.example.com";
  const ptrIP = ipv4.trim() || "203.0.113.10";
  return (
    <InfoNote>
      <div className="font-medium text-ink">Reverse DNS (PTR)</div>
      <ol className="mt-2 list-decimal space-y-1.5 pl-4">
        <li>Open your VPS provider's IP or networking panel.</li>
        <li>
          Find <span className="text-ink">Reverse DNS</span>, <span className="text-ink">rDNS</span>, or{" "}
          <span className="text-ink">PTR</span> for <Identifier>{ptrIP}</Identifier>.
        </li>
        <li>
          Set it to <Identifier>{ptrHost}</Identifier>. This is not created at your domain DNS provider.
        </li>
        <li>
          Check it later with <Identifier className="break-all leading-relaxed">dig -x {ptrIP} +short</Identifier>.
        </li>
      </ol>
    </InfoNote>
  );
}

/* step 4: first domain */
function StepDomain({ onNext }: { onNext: (d: Domain) => void }) {
  const [name, setName] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await post<{ domain: Domain }>("/api/setup/domain", {
        name: name.trim().toLowerCase(),
      });
      onNext(res.domain);
    } catch (err: any) {
      setError(err.message);
      setBusy(false);
    }
  }

  return (
    <StepCard title="Add your first domain">
      <form onSubmit={submit} className="space-y-4">
        <Field label="Domain">
          <Input autoFocus required placeholder="example.com" value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        {name && (
          <InfoNote>
            Mail for <Identifier>@{name}</Identifier> · webmail at{" "}
            <Identifier>https://mail.{name}</Identifier>
          </InfoNote>
        )}
        <ErrorNote>{error}</ErrorNote>
        <div className="flex justify-end">
          <Button type="submit" variant="primary" busy={busy}>
            Add domain <ArrowRight size={14} />
          </Button>
        </div>
      </form>
    </StepCard>
  );
}

/* steps 5–6: sending method + relay config */
function StepDelivery({
  outbound25Open,
  onNext,
}: {
  outbound25Open: boolean | null;
  onNext: () => void;
}) {
  // Presets come from the server (delivery.Presets) so there is one source of
  // truth shared with the admin console — no hand-maintained copy here.
  const presets = useLoad(() => get<{ presets: RelayPreset[] }>("/api/setup/relay-presets"));
  if (!presets.data) {
    return (
      <StepCard title="How should mail leave this server?">
        {presets.error ? <ErrorNote>{presets.error}</ErrorNote> : <Spinner />}
      </StepCard>
    );
  }
  return <StepDeliveryForm presets={presets.data.presets} outbound25Open={outbound25Open} onNext={onNext} />;
}

function StepDeliveryForm({
  presets,
  outbound25Open,
  onNext,
}: {
  presets: RelayPreset[];
  outbound25Open: boolean | null;
  onNext: () => void;
}) {
  const outbound25Blocked = outbound25Open === false;
  const directBlockedMessage =
    "Outbound port 25 is not available on this server. Choose SMTP relay instead.";
  const [mode, setMode] = useState<"direct" | "relay">(outbound25Blocked ? "relay" : "direct");
  const [provider, setProvider] = useState(presets[0]?.provider ?? "custom");
  const preset = useMemo(
    () => presets.find((p) => p.provider === provider) ?? presets[0],
    [presets, provider],
  );
  const [host, setHost] = useState(preset.host);
  const [port, setPort] = useState(String(preset.port));
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (outbound25Blocked) setMode("relay");
  }, [outbound25Blocked]);

  function pick(p: string) {
    setProvider(p);
    const pr = presets.find((x) => x.provider === p)!;
    setHost(pr.host);
    setPort(String(pr.port));
  }

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (mode === "direct" && outbound25Blocked) {
      setError(directBlockedMessage);
      return;
    }
    setBusy(true);
    setError("");
    try {
      await post("/api/setup/delivery", {
        mode,
        relay:
          mode === "relay"
            ? {
                name: preset.label,
                provider,
                host,
                port: Number(port),
                username,
                password,
                tls_mode: preset.tls_mode,
              }
            : undefined,
      });
      onNext();
    } catch (err: any) {
      setError(err.message);
      setBusy(false);
    }
  }

  return (
    <StepCard title="How should mail leave this server?">
      <form onSubmit={submit} className="space-y-4">
        {outbound25Blocked && (
          <InfoNote>
            Outbound port 25 looks blocked on this server. Relay mode is selected so sending still works.
          </InfoNote>
        )}
        <div className="grid gap-3 sm:grid-cols-2">
          <ModeCard
            active={mode === "direct"}
            onClick={() => {
              if (outbound25Blocked) {
                setError(directBlockedMessage);
                return;
              }
              setError("");
              setMode("direct");
            }}
            title="Direct"
            body={
              outbound25Blocked
                ? "Outbound port 25 is blocked; use only if your provider opens it."
                : "Requires outbound port 25, PTR, and a clean IP."
            }
          />
          <ModeCard
            active={mode === "relay"}
            onClick={() => {
              setError("");
              setMode("relay");
            }}
            title="Through a relay"
            body="Use SES, Postmark, Mailgun, SMTP2GO, Resend, or custom SMTP."
          />
        </div>
        {mode === "direct" && !outbound25Blocked && (
          <InfoNote>
            Direct sending needs reverse DNS (PTR) to point your server IP back to the mail hostname. If your VPS
            provider cannot set it, choose relay.
          </InfoNote>
        )}

        {mode === "relay" && (
          <div className="space-y-4 rounded-xl border border-line bg-inset/60 p-4">
            <Field label="Provider">
              <Select value={provider} onChange={(e) => pick(e.target.value)}>
                {presets.map((p) => (
                  <option key={p.provider} value={p.provider}>
                    {p.label}
                  </option>
                ))}
              </Select>
            </Field>
            {preset.note && <InfoNote>{preset.note}</InfoNote>}
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="Host">
                <Input required value={host} onChange={(e) => setHost(e.target.value)} />
              </Field>
              <Field label="Port">
                <Input required value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))} />
              </Field>
              <Field label="Username" hint={preset.username_hint}>
                <Input value={username} onChange={(e) => setUsername(e.target.value)} />
              </Field>
              <Field label="Password">
                <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
              </Field>
            </div>
          </div>
        )}
        <ErrorNote>{error}</ErrorNote>
        <div className="flex justify-end">
          <Button type="submit" variant="primary" busy={busy}>
            Continue <ArrowRight size={14} />
          </Button>
        </div>
      </form>
    </StepCard>
  );
}

function ModeCard({
  active,
  onClick,
  title,
  body,
}: {
  active: boolean;
  onClick: () => void;
  title: string;
  body: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-xl border p-4 text-left transition-all ${
        active
          ? "border-accent/60 bg-accent-dim shadow-[0_0_20px_var(--glow)]"
          : "border-line bg-inset/40 hover:border-line-strong"
      }`}
    >
      <div className={`text-[14px] font-semibold ${active ? "text-accent" : "text-ink"}`}>{title}</div>
      <div className="mt-1 text-[12.5px] leading-relaxed text-muted">{body}</div>
    </button>
  );
}

/* steps 7–8: DNS records + check */
function StepDns({ domain, onNext }: { domain: Domain; onNext: () => void }) {
  const [records, setRecords] = useState<DnsRecord[] | null>(null);
  const [error, setError] = useState("");
  const [checking, setChecking] = useState(false);

  useEffect(() => {
    get<{ records: DnsRecord[] }>(`/api/setup/dns/${domain.id}`)
      .then((r) => setRecords(r.records))
      .catch((e) => setError(e.message));
  }, [domain.id]);

  async function check() {
    setChecking(true);
    try {
      const res = await post<{ records: DnsRecord[] }>(`/api/setup/dns/${domain.id}/check`);
      setRecords(res.records);
      const essential = res.records.filter((r) => r.purpose === "a" || r.purpose === "mx");
      if (essential.every((r) => r.status === "ok")) toast("DNS looks good!");
      else toast("Some records aren't visible yet — DNS can take a few minutes to propagate", "error");
    } catch (e: any) {
      toast(e.message, "error");
    } finally {
      setChecking(false);
    }
  }

  const essentialOk =
    records?.filter((r) => r.purpose === "a" || r.purpose === "mx").every((r) => r.status === "ok") ??
    false;

  return (
    <StepCard
      title="Create these DNS records"
      lead={`Add these at your DNS provider for ${domain.name}. A and MX are required now.`}
    >
      {error && <ErrorNote>{error}</ErrorNote>}
      {!records && !error && <Spinner />}
      <div className="space-y-3">
        {records?.map((r) => (
          <div key={r.purpose + r.name} className="rounded-lg border border-line bg-inset/50 p-3">
            <div className="flex items-center justify-between gap-2">
              <div className="flex min-w-0 items-center gap-2">
                <span className="rounded border border-line bg-raised px-1.5 py-0.5 font-mono text-[11px] font-semibold text-ink">
                  {r.type}
                </span>
                <Identifier muted className="truncate text-[12px]">{r.name}</Identifier>
              </div>
              <StatusPill status={r.status || "unknown"} label={dnsStatusLabel[r.status || ""]} />
            </div>
            <div className="mt-2 flex items-center gap-1">
              <code className="flex-1 select-all break-all font-mono text-[11.5px] leading-relaxed text-ink/80">
                {r.value}
              </code>
              <CopyButton text={r.value} />
            </div>
          </div>
        ))}
      </div>
      <div className="mt-5 flex items-center justify-between gap-3">
        <RefreshButton onClick={check} busy={checking}>
          Check DNS
        </RefreshButton>
        <div className="flex items-center gap-3">
          {!essentialOk && (
            <span className="text-[12px] text-faint">DNS can be fixed later.</span>
          )}
          <Button variant="primary" onClick={onNext}>
            Continue <ArrowRight size={14} />
          </Button>
        </div>
      </div>
    </StepCard>
  );
}

/* step 9: certificate */
function StepCertificate({ domain, onNext }: { domain: Domain; onNext: () => void }) {
  const [cert, setCert] = useState<Certificate | null>(null);
  const [error, setError] = useState("");
  const [started, setStarted] = useState(false);

  async function start() {
    setStarted(true);
    setError("");
    try {
      const res = await post<{ certificate: Certificate }>("/api/setup/certificate", {
        domain_id: domain.id,
      });
      setCert(res.certificate);
    } catch (e: any) {
      setError(e.message);
      setStarted(false);
    }
  }

  useEffect(() => {
    if (!cert || cert.status === "active" || cert.status === "error" || cert.status === "dns_wait")
      return;
    const t = setInterval(async () => {
      try {
        const res = await get<{ certificate: Certificate }>(`/api/setup/certificate/${cert.id}`);
        setCert(res.certificate);
      } catch {
        /* keep polling */
      }
    }, 2000);
    return () => clearInterval(t);
  }, [cert]);

  return (
    <StepCard
      title="Issue the certificate"
      lead={`Requests a Let's Encrypt certificate for ${domain.mail_hostname}.`}
    >
      {!started && (
        <div className="flex justify-center py-4">
          <Button variant="primary" onClick={start}>
            Request certificate
          </Button>
        </div>
      )}
      {cert && (
        <div className="flex items-center gap-3 rounded-lg border border-line bg-inset/60 px-4 py-3">
          {cert.status === "active" ? (
            <CheckCircle2 size={18} className="text-ok" />
          ) : cert.status === "error" || cert.status === "dns_wait" ? (
            <XCircle size={18} className="text-danger" />
          ) : (
            <Loader2 size={18} className="animate-spin text-accent" />
          )}
          <div className="min-w-0 flex-1">
            <Identifier className="block">{cert.hostname}</Identifier>
            <div className="text-[12px] text-muted">
              {cert.status === "active" && "Certificate issued and installed."}
              {cert.status === "issuing" && "Talking to the certificate authority…"}
              {cert.status === "pending" && "Starting…"}
              {(cert.status === "error" || cert.status === "dns_wait") && (cert.last_error || "Issuance failed.")}
            </div>
          </div>
          {(cert.status === "error" || cert.status === "dns_wait") && (
            <Button size="sm" onClick={start}>
              Retry
            </Button>
          )}
        </div>
      )}
      <ErrorNote>{error}</ErrorNote>
      <div className="mt-5 flex items-center justify-between">
        <span className="text-[12px] text-faint">
          {cert?.status !== "active" && "You can continue — issuance retries automatically."}
        </span>
        <Button variant="primary" onClick={onNext} disabled={!started && !cert}>
          Continue <ArrowRight size={14} />
        </Button>
      </div>
    </StepCard>
  );
}

/* step 10: first mailbox */
function StepMailbox({ domain, onNext }: { domain: Domain; onNext: () => void }) {
  const [localPart, setLocalPart] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await post("/api/setup/mailbox", {
        domain_id: domain.id,
        local_part: localPart.trim().toLowerCase(),
        password,
      });
      onNext();
    } catch (err: any) {
      setError(err.message);
      setBusy(false);
    }
  }

  return (
    <StepCard title="Create your mailbox">
      <form onSubmit={submit} className="space-y-4">
        <Field label="Address">
          <div className="flex items-center gap-2">
            <Input autoFocus required placeholder="you" value={localPart} onChange={(e) => setLocalPart(e.target.value)} className="flex-1" />
            <span className="whitespace-nowrap text-[13.5px] text-muted">@{domain.name}</span>
          </div>
        </Field>
        <Field label="Password" hint="At least 10 characters.">
          <Input type="password" required value={password} onChange={(e) => setPassword(e.target.value)} />
        </Field>
        <ErrorNote>{error}</ErrorNote>
        <div className="flex justify-end">
          <Button type="submit" variant="primary" busy={busy}>
            Create mailbox <ArrowRight size={14} />
          </Button>
        </div>
      </form>
    </StepCard>
  );
}

/* step 11: test email */
function StepTestEmail({ onNext }: { onNext: () => void }) {
  const [to, setTo] = useState("");
  const [result, setResult] = useState<null | { ok: boolean; error?: string }>(null);
  const [busy, setBusy] = useState(false);

  return (
    <StepCard
      title="Send a test email"
      lead="Uses the active delivery policy."
    >
      <form
        className="space-y-4"
        onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setResult(null);
          try {
            const res = await post<{ ok: boolean; error?: string }>("/api/setup/test-email", { to });
            setResult(res);
          } catch (err: any) {
            setResult({ ok: false, error: err.message });
          } finally {
            setBusy(false);
          }
        }}
      >
        <Field label="Send to">
          <Input type="email" autoFocus required placeholder="you@gmail.com" value={to} onChange={(e) => setTo(e.target.value)} />
        </Field>
        {result &&
          (result.ok ? (
            <InfoNote>Test message handed to the mail queue — check the inbox (and spam folder) at {to}.</InfoNote>
          ) : (
            <ErrorNote>{result.error}</ErrorNote>
          ))}
        <div className="flex items-center justify-between">
          <Button type="submit" busy={busy}>
            <Mail size={13} /> Send test
          </Button>
          <Button variant="primary" onClick={onNext} type="button">
            {result?.ok ? "Continue" : "Skip for now"} <ArrowRight size={14} />
          </Button>
        </div>
      </form>
    </StepCard>
  );
}

/* step 12: done */
function StepDone() {
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState("");

  async function finish() {
    setBusy(true);
    setError("");
    try {
      await post("/api/setup/complete");
      setDone(true);
      setTimeout(() => {
        window.location.href = "/";
      }, 1600);
    } catch (e: any) {
      setError(e.message);
      setBusy(false);
    }
  }

  return (
    <StepCard
      title={done ? "Opening webmail" : "Everything is ready"}
      lead={
        done
          ? undefined
          : "Completing setup disables /setup."
      }
    >
      <ul className="space-y-2 text-[13px] text-muted">
        <li>• Webmail lives at <span className="font-mono text-ink">/</span></li>
        <li>• Server administration at <span className="font-mono text-ink">/admin</span></li>
        <li>• IMAP on port 993, SMTP submission on 587 — use your mailbox credentials</li>
      </ul>
      <ErrorNote>{error}</ErrorNote>
      <div className="mt-6 flex justify-end">
        <Button variant="primary" onClick={finish} busy={busy} disabled={done}>
          {done ? <Check size={14} /> : "Finish and open webmail"}
        </Button>
      </div>
    </StepCard>
  );
}
