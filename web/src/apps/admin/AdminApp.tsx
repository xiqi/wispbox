import { type FormEvent, useEffect, useState } from "react";
import {
  Activity,
  Archive,
  AtSign,
  Database,
  FileClock,
  Globe,
  Inbox,
  KeyRound,
  ListTree,
  Lock,
  LogOut,
  Mails,
  Menu,
  Send,
  Settings2,
  ShieldCheck,
  X,
} from "lucide-react";
import { startAuthentication } from "@simplewebauthn/browser";
import { clearCsrf, get, post, setCsrf } from "../../lib/api";
import { useBrand } from "../../lib/brand";
import { usePath } from "../../lib/hooks";
// setCsrf is used by both the login form and the session probe below.
import {
  BrandMark,
  Button,
  ErrorNote,
  Field,
  IconButton,
  Input,
  Spinner,
  ThemeToggle,
  Wordmark,
} from "../../components/ui";
import Overview from "./sections/Overview";
import Domains from "./sections/Domains";
import Mailboxes from "./sections/Mailboxes";
import Aliases from "./sections/Aliases";
import Delivery from "./sections/Delivery";
import Dns from "./sections/Dns";
import Certificates from "./sections/Certificates";
import Queue from "./sections/Queue";
import Logs from "./sections/Logs";
import Security from "./sections/Security";
import Backup from "./sections/Backup";
import Settings from "./sections/Settings";

const sections = [
  { id: "overview", label: "Overview", icon: Activity, el: Overview },
  { id: "domains", label: "Domains", icon: Globe, el: Domains },
  { id: "mailboxes", label: "Mailboxes", icon: Inbox, el: Mailboxes },
  { id: "aliases", label: "Aliases", icon: AtSign, el: Aliases },
  { id: "delivery", label: "Delivery", icon: Send, el: Delivery },
  { id: "dns", label: "DNS", icon: ListTree, el: Dns },
  { id: "certificates", label: "Certificates", icon: ShieldCheck, el: Certificates },
  { id: "queue", label: "Queue", icon: Mails, el: Queue },
  { id: "logs", label: "Logs", icon: FileClock, el: Logs },
  { id: "security", label: "Security", icon: Lock, el: Security },
  { id: "backup", label: "Backup", icon: Archive, el: Backup },
  { id: "settings", label: "Settings", icon: Settings2, el: Settings },
] as const;

export default function AdminApp() {
  const brand = useBrand();
  const [me, setMe] = useState<string | null | undefined>(undefined);
  const [path, navigate] = usePath();
  const [navOpen, setNavOpen] = useState(false);

  // Session probe; also refreshes the (stateless) CSRF token so mutations
  // keep working after a browser restart clears sessionStorage.
  useEffect(() => {
    document.title = `${brand.name} Admin`;
  }, [brand.name]);

  useEffect(() => {
    get<{ username: string; csrf: string }>("/api/admin/me")
      .then((r) => {
        if (r.csrf) setCsrf(r.csrf);
        setMe(r.username);
      })
      .catch(() => setMe(null));
  }, []);

  if (me === undefined) return <Spinner label="Loading…" />;
  if (me === null) return <AdminLogin onLogin={setMe} />;

  const active =
    sections.find((s) => path === `/admin/${s.id}` || path.startsWith(`/admin/${s.id}/`)) ??
    sections[0];
  const ActiveEl = active.el;

  return (
    <div className="flex h-full overflow-hidden bg-bg">
      {/* mobile scrim */}
      {navOpen && (
        <div className="fixed inset-0 z-20 bg-black/50 md:hidden" onClick={() => setNavOpen(false)} />
      )}
      <aside
        className={`${navOpen ? "translate-x-0" : "-translate-x-full"} fixed inset-y-0 left-0 z-30 flex w-[228px] flex-col border-r border-line bg-bg-deep transition-transform md:static md:translate-x-0`}
      >
        <div className="flex items-center justify-between px-4 pb-2 pt-4">
          <Wordmark sub="admin" />
          <IconButton
            title="Close navigation"
            size="md"
            className="md:hidden"
            onClick={() => setNavOpen(false)}
            icon={<X size={16} />}
          />
        </div>
        <nav className="flex-1 space-y-0.5 overflow-y-auto px-3 py-3">
          {sections.map((s) => {
            const Icon = s.icon;
            const isActive = s.id === active.id;
            return (
              <button
                key={s.id}
                onClick={() => {
                  navigate(`/admin/${s.id}`);
                  setNavOpen(false);
                }}
                className={`flex w-full items-center gap-2.5 rounded-lg px-2.5 py-[7px] text-[13.5px] transition-colors ${
                  isActive
                    ? "bg-accent-dim font-medium text-accent"
                    : "text-muted hover:bg-inset hover:text-ink"
                }`}
              >
                <Icon size={15} strokeWidth={isActive ? 2.2 : 1.8} />
                {s.label}
              </button>
            );
          })}
        </nav>
        <footer className="border-t border-line px-4 py-3">
          <div className="flex items-center justify-between gap-2">
            <div className="min-w-0">
              <div className="truncate text-[12.5px] font-medium text-ink">{me}</div>
            </div>
            <div className="flex items-center">
              <ThemeToggle />
              <IconButton
                title="Sign out"
                onClick={async () => {
                  try {
                    await post("/api/admin/logout");
                  } finally {
                    clearCsrf();
                    setMe(null);
                  }
                }}
                size="md"
                icon={<LogOut size={14} />}
              />
            </div>
          </div>
        </footer>
      </aside>

      <main className="flex min-w-0 flex-1 flex-col">
        <header className="flex items-center gap-3 border-b border-line px-5 py-3 md:px-8">
          <IconButton
            title="Open navigation"
            size="md"
            className="md:hidden"
            onClick={() => setNavOpen(true)}
            icon={<Menu size={18} />}
          />
          <h1 className="text-[15px] font-semibold text-ink">{active.label}</h1>
        </header>
        <div className="flex-1 overflow-y-auto px-5 py-5 md:px-8 md:py-6">
          <div className="mx-auto w-full max-w-5xl">
            <ActiveEl />
          </div>
        </div>
      </main>
    </div>
  );
}

function AdminLogin({ onLogin }: { onLogin: (username: string) => void }) {
  const brand = useBrand();
  const [username, setUsername] = useState("");
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
      const res = await post<{ username?: string; csrf?: string; two_factor_required?: boolean }>("/api/admin/login", {
        username,
        password,
        totp_code: totpCode,
      });
      if (res.two_factor_required) {
        setTotpRequired(true);
        return;
      }
      if (res.csrf && res.username) {
        setCsrf(res.csrf);
        onLogin(res.username);
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
      const begin = await post<{ challenge_id: string; options: any }>("/api/admin/passkeys/login/options");
      const credential = await startAuthentication({ optionsJSON: begin.options });
      const res = await post<{ username: string; csrf: string }>(
        `/api/admin/passkeys/login/finish?challenge_id=${encodeURIComponent(begin.challenge_id)}`,
        credential,
      );
      setCsrf(res.csrf);
      onLogin(res.username);
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
          <h1 className="max-w-full truncate text-[22px] font-semibold text-ink">
            {brand.name} <span className="font-normal text-muted">admin</span>
          </h1>
        </div>
        <form
          onSubmit={submit}
          className="space-y-4 rounded-xl border border-line bg-raised/80 p-6 shadow-pop backdrop-blur"
        >
          <Field label="Admin username">
            <Input
              type="text"
              autoFocus
              required
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
            />
          </Field>
          <Field label="Password">
            <Input
              type="password"
              required
              autoComplete="current-password"
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
