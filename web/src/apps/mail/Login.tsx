import { type FormEvent, useState } from "react";
import { KeyRound } from "lucide-react";
import { startAuthentication } from "@simplewebauthn/browser";
import { post, setCsrf } from "../../lib/api";
import { useBrand } from "../../lib/brand";
import { BrandMark, Button, ErrorNote, Field, Input, ThemeToggle } from "../../components/ui";

export default function Login({ onLogin }: { onLogin: (email: string) => void }) {
  const brand = useBrand();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [totpCode, setTotpCode] = useState("");
  const [totpRequired, setTotpRequired] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [passkeyBusy, setPasskeyBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const res = await post<{ email?: string; csrf?: string; two_factor_required?: boolean }>("/api/mail/login", {
        email,
        password,
        totp_code: totpCode,
      });
      if (res.two_factor_required) {
        setTotpRequired(true);
        return;
      }
      if (res.csrf && res.email) {
        setCsrf(res.csrf);
        onLogin(res.email);
      }
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  }

  async function passkeySignIn() {
    setPasskeyBusy(true);
    setError("");
    try {
      const begin = await post<{ challenge_id: string; options: any }>("/api/mail/passkeys/login/options");
      const credential = await startAuthentication({ optionsJSON: begin.options });
      const res = await post<{ email: string; csrf: string }>(
        `/api/mail/passkeys/login/finish?challenge_id=${encodeURIComponent(begin.challenge_id)}`,
        credential,
      );
      setCsrf(res.csrf);
      onLogin(res.email);
    } catch (err: any) {
      setError(err.message);
    } finally {
      setPasskeyBusy(false);
    }
  }

  return (
    <div className="wisp-atmosphere flex min-h-full items-center justify-center p-6">
      <div className="absolute right-4 top-4">
        <ThemeToggle />
      </div>
      <div className="animate-rise w-full max-w-[380px]">
        <div className="mb-8 flex flex-col items-center gap-3">
          <BrandMark size={40} />
          <h1 className="max-w-full truncate text-[22px] font-semibold text-ink">{brand.name}</h1>
        </div>
        <form
          onSubmit={submit}
          className="space-y-4 rounded-xl border border-line bg-raised/80 p-6 shadow-pop backdrop-blur"
        >
          <Field label="Email">
            <Input
              type="email"
              autoComplete="username"
              autoFocus
              required
              placeholder="you@example.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
          </Field>
          <Field label="Password">
            <Input
              type="password"
              autoComplete="current-password"
              required
              placeholder="••••••••••"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </Field>
          {totpRequired && (
            <Field label="Verification code">
              <Input
                autoFocus
                inputMode="numeric"
                autoComplete="one-time-code"
                value={totpCode}
                onChange={(e) => setTotpCode(e.target.value)}
              />
            </Field>
          )}
          <ErrorNote>{error}</ErrorNote>
          <Button type="submit" variant="primary" busy={busy} className="w-full">
            {totpRequired ? "Verify and sign in" : "Sign in"}
          </Button>
          <Button type="button" className="w-full" busy={passkeyBusy} onClick={passkeySignIn}>
            <KeyRound size={14} />
            Use passkey
          </Button>
        </form>
      </div>
    </div>
  );
}
