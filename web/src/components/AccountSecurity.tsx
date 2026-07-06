import { type FormEvent, useEffect, useState } from "react";
import { KeyRound, ShieldCheck, Trash2 } from "lucide-react";
import { startRegistration } from "@simplewebauthn/browser";
import { del, get, post } from "../lib/api";
import { Button, Card, CopyButton, ErrorNote, Field, Input, Spinner, Td, Th, Table, toast } from "./ui";

type Passkey = {
  id: number;
  name: string;
  rp_id: string;
  created_at: string;
  last_used_at: string;
};

type SecurityState = {
  two_factor_enabled: boolean;
  passkeys: Passkey[];
};

type TOTPSetup = {
  challenge_id: string;
  secret: string;
  otpauth_uri: string;
};

export default function AccountSecurity({ base }: { base: "/api/admin" | "/api/mail" }) {
  const [data, setData] = useState<SecurityState | null>(null);
  const [busy, setBusy] = useState(true);
  const [error, setError] = useState("");

  async function reload() {
    setBusy(true);
    setError("");
    try {
      const next = await get<SecurityState>(`${base}/account/security`);
      setData({ ...next, passkeys: next.passkeys ?? [] });
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    reload();
  }, [base]);

  if (busy && !data) return <Spinner />;
  return (
    <div className="space-y-4">
      <ErrorNote>{error}</ErrorNote>
      <PasswordPanel base={base} />
      {data && <TOTPPanel base={base} enabled={data.two_factor_enabled} onChanged={reload} />}
      {data && <PasskeyPanel base={base} passkeys={data.passkeys} onChanged={reload} />}
    </div>
  );
}

function PasswordPanel({ base }: { base: string }) {
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await post(`${base}/account/password`, {
        current_password: currentPassword,
        new_password: newPassword,
      });
      setCurrentPassword("");
      setNewPassword("");
      toast("Password updated");
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card title="Password">
      <form onSubmit={submit} className="grid gap-3 md:grid-cols-[1fr_1fr_auto] md:items-end">
        <Field label="Current password">
          <Input type="password" autoComplete="current-password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} />
        </Field>
        <Field label="New password">
          <Input type="password" autoComplete="new-password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} />
        </Field>
        <Button type="submit" variant="primary" busy={busy} disabled={!currentPassword || !newPassword}>
          Update
        </Button>
      </form>
      <div className="mt-3">
        <ErrorNote>{error}</ErrorNote>
      </div>
    </Card>
  );
}

function TOTPPanel({ base, enabled, onChanged }: { base: string; enabled: boolean; onChanged: () => void }) {
  const [code, setCode] = useState("");
  const [setup, setSetup] = useState<TOTPSetup | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function startSetup() {
    setBusy(true);
    setError("");
    try {
      setSetup(await post<TOTPSetup>(`${base}/account/2fa/setup`));
      setCode("");
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function enable() {
    if (!setup) return;
    setBusy(true);
    setError("");
    try {
      await post(`${base}/account/2fa/enable`, { challenge_id: setup.challenge_id, code });
      setSetup(null);
      setCode("");
      toast("Two-factor authentication enabled");
      onChanged();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function disable() {
    setBusy(true);
    setError("");
    try {
      await post(`${base}/account/2fa/disable`, { code });
      setCode("");
      toast("Two-factor authentication disabled");
      onChanged();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card
      title={
        <span className="inline-flex items-center gap-2">
          <ShieldCheck size={14} /> 2FA
        </span>
      }
    >
      <div className="space-y-3">
        <div className={`inline-flex h-6 items-center rounded-full border px-2 text-[12px] ${enabled ? "border-ok/30 text-ok" : "border-line text-muted"}`}>
          {enabled ? "Enabled" : "Not enabled"}
        </div>
        {!enabled && !setup && (
          <div className="flex">
            <Button type="button" variant="primary" busy={busy} onClick={startSetup}>
              Set up
            </Button>
          </div>
        )}
        {!enabled && setup && (
          <div className="space-y-3">
            <div className="rounded-lg border border-line bg-inset p-3">
              <div className="flex items-center justify-between gap-2">
                <div className="min-w-0 truncate font-mono text-[12px] text-ink">{setup.secret}</div>
                <CopyButton text={setup.secret} />
              </div>
              <div className="mt-2 flex items-center justify-between gap-2">
                <div className="min-w-0 truncate text-[12px] text-faint">{setup.otpauth_uri}</div>
                <CopyButton text={setup.otpauth_uri} />
              </div>
            </div>
            <div className="grid gap-3 md:grid-cols-[1fr_auto] md:items-end">
              <Field label="Verification code">
                <Input inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} />
              </Field>
              <Button type="button" variant="primary" busy={busy} disabled={!code} onClick={enable}>
                Enable
              </Button>
            </div>
          </div>
        )}
        {enabled && (
          <div className="grid gap-3 md:grid-cols-[10rem_auto] md:items-end">
            <Field label="Code">
              <Input inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} />
            </Field>
            <Button type="button" variant="danger" busy={busy} disabled={!code} onClick={disable}>
              Disable
            </Button>
          </div>
        )}
        <ErrorNote>{error}</ErrorNote>
      </div>
    </Card>
  );
}

function PasskeyPanel({ base, passkeys, onChanged }: { base: string; passkeys: Passkey[]; onChanged: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function addPasskey() {
    setBusy(true);
    setError("");
    try {
      const begin = await post<{ challenge_id: string; options: any }>(`${base}/account/passkeys/register/options`);
      const credential = await startRegistration({ optionsJSON: begin.options });
      await post(`${base}/account/passkeys/register/finish?challenge_id=${encodeURIComponent(begin.challenge_id)}`, credential);
      toast("Passkey added");
      onChanged();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function removePasskey(id: number) {
    setBusy(true);
    setError("");
    try {
      await del(`${base}/account/passkeys/${id}`);
      toast("Passkey removed");
      onChanged();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card
      title={
        <span className="inline-flex items-center gap-2">
          <KeyRound size={14} /> Passkeys
        </span>
      }
    >
      <div className="space-y-3">
        <div className="flex">
          <Button type="button" variant="primary" busy={busy} onClick={addPasskey}>
            Add passkey
          </Button>
        </div>
        {passkeys.length > 0 && (
          <Table
            head={
              <>
                <Th>Name</Th>
                <Th>Host</Th>
                <Th>Created</Th>
                <Th />
              </>
            }
          >
            {passkeys.map((p) => (
              <tr key={p.id}>
                <Td>{p.name}</Td>
                <Td className="font-mono text-[12px] text-muted">{p.rp_id}</Td>
                <Td className="whitespace-nowrap text-muted">{p.created_at.slice(0, 10)}</Td>
                <Td className="text-right">
                  <button
                    type="button"
                    className="inline-flex h-8 w-8 items-center justify-center rounded-lg text-faint hover:bg-danger-dim hover:text-danger"
                    onClick={() => removePasskey(p.id)}
                    title="Delete passkey"
                  >
                    <Trash2 size={14} />
                  </button>
                </Td>
              </tr>
            ))}
          </Table>
        )}
        <ErrorNote>{error}</ErrorNote>
      </div>
    </Card>
  );
}
