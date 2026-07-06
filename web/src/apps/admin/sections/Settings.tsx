import { type ChangeEvent, type FormEvent, useEffect, useRef, useState } from "react";
import { del, get, patch, post, postForm } from "../../../lib/api";
import { brandFromSettings, useBrand } from "../../../lib/brand";
import { useLoad } from "../../../lib/hooks";
import type { UpgradeStatus } from "../../../lib/types";
import {
  BrandMark,
  Button,
  Card,
  ErrorNote,
  Field,
  Input,
  Spinner,
  toast,
} from "../../../components/ui";

export default function Settings() {
  const brand = useBrand();
  const { data, error, busy, reload } = useLoad(() =>
    get<{ settings: Record<string, string> }>("/api/admin/settings"),
  );
  const logoInputRef = useRef<HTMLInputElement | null>(null);
  const [acmeEmail, setAcmeEmail] = useState("");
  const [ipv4, setIpv4] = useState("");
  const [ipv6, setIpv6] = useState("");
  const [brandName, setBrandName] = useState("");
  const [logo, setLogo] = useState("");
  const [saving, setSaving] = useState(false);
  const [savingBrand, setSavingBrand] = useState(false);
  const [uploadingLogo, setUploadingLogo] = useState(false);

  useEffect(() => {
    if (data) {
      setAcmeEmail(data.settings.acme_email ?? "");
      setIpv4(data.settings.server_ipv4 ?? "");
      setIpv6(data.settings.server_ipv6 ?? "");
      setBrandName(data.settings.brand_name ?? "");
      setLogo(data.settings.brand_logo ?? "");
    }
  }, [data]);

  if (busy && !data) return <Spinner />;
  if (error) return <ErrorNote>{error}</ErrorNote>;

  async function save(e: FormEvent) {
    e.preventDefault();
    setSaving(true);
    try {
      await patch("/api/admin/settings", {
        acme_email: acmeEmail,
        server_ipv4: ipv4,
        server_ipv6: ipv6,
      });
      toast("Settings saved");
      reload();
    } catch (err: any) {
      toast(err.message, "error");
    } finally {
      setSaving(false);
    }
  }

  async function saveBrand(e: FormEvent) {
    e.preventDefault();
    setSavingBrand(true);
    try {
      const res = await patch<{ settings: Record<string, string> }>("/api/admin/settings", {
        brand_name: brandName,
      });
      const next = brandFromSettings(res.settings);
      setBrandName(res.settings.brand_name ?? "");
      setLogo(res.settings.brand_logo ?? "");
      brand.setBrand(next);
      toast("Appearance saved");
      reload();
    } catch (err: any) {
      toast(err.message, "error");
    } finally {
      setSavingBrand(false);
    }
  }

  async function uploadLogo(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;
    setUploadingLogo(true);
    try {
      const form = new FormData();
      form.append("logo", file);
      const res = await postForm<{ settings: Record<string, string> }>("/api/admin/settings/logo", form);
      const next = brandFromSettings(res.settings);
      setLogo(res.settings.brand_logo ?? "");
      brand.setBrand(next);
      toast("Logo updated");
      reload();
    } catch (err: any) {
      toast(err.message, "error");
    } finally {
      setUploadingLogo(false);
      if (logoInputRef.current) logoInputRef.current.value = "";
    }
  }

  async function removeLogo() {
    setUploadingLogo(true);
    try {
      const res = await del<{ settings: Record<string, string> }>("/api/admin/settings/logo");
      const next = brandFromSettings(res.settings);
      setLogo("");
      brand.setBrand(next);
      toast("Logo removed");
      reload();
    } catch (err: any) {
      toast(err.message, "error");
    } finally {
      setUploadingLogo(false);
    }
  }

  return (
    <div className="space-y-5">
      <Card title="Appearance">
        <div className="max-w-lg space-y-4">
          <div className="flex items-center gap-4">
            <div className="flex h-16 w-24 items-center justify-center rounded-lg border border-line bg-inset px-3">
              <BrandMark size={34} />
            </div>
            <div className="min-w-0">
              <div className="truncate text-[13.5px] font-medium text-ink">{brand.name}</div>
              <div className="mt-1 text-[12.5px] leading-relaxed text-muted">
                Shown in webmail, admin, setup, and browser tabs.
              </div>
            </div>
          </div>

          <form onSubmit={saveBrand} className="space-y-4">
            <Field label="System name" hint="Leave empty to use wispbox.">
              <Input
                value={brandName}
                maxLength={40}
                onChange={(e) => setBrandName(e.target.value)}
                placeholder="wispbox"
              />
            </Field>
            <Button type="submit" variant="primary" busy={savingBrand}>
              Save appearance
            </Button>
          </form>

          <Field label="Logo" hint="PNG, JPEG, or WebP. 256 KB max.">
            <input
              ref={logoInputRef}
              type="file"
              accept="image/png,image/jpeg,image/webp"
              disabled={uploadingLogo}
              onChange={uploadLogo}
              className="block w-full text-[13px] text-muted file:mr-3 file:h-8 file:rounded-lg file:border file:border-line-strong file:bg-inset file:px-3 file:text-[12.5px] file:font-medium file:text-ink hover:file:bg-raised disabled:opacity-50"
            />
          </Field>
          <Button type="button" onClick={removeLogo} disabled={!logo || uploadingLogo} busy={uploadingLogo}>
            Remove logo
          </Button>
        </div>
      </Card>

      <UpdatesCard />

      <Card title="Server">
        <form onSubmit={save} className="max-w-lg space-y-4">
          <Field label="Primary hostname">
            <Input disabled value={data?.settings.primary_hostname ?? ""} />
          </Field>
          <Field
            label="Server IPv4"
            hint="Used to verify DNS before requesting certificates and to build your A records."
          >
            <Input value={ipv4} onChange={(e) => setIpv4(e.target.value)} placeholder="203.0.113.10" />
          </Field>
          <Field label="Server IPv6 (optional)">
            <Input value={ipv6} onChange={(e) => setIpv6(e.target.value)} placeholder="2001:db8::1" />
          </Field>
          <Field
            label="Certificate contact email"
            hint="Let's Encrypt sends expiry warnings here if renewal keeps failing."
          >
            <Input
              type="email"
              value={acmeEmail}
              onChange={(e) => setAcmeEmail(e.target.value)}
              placeholder="admin@example.com"
            />
          </Field>
          <Button type="submit" variant="primary" busy={saving}>
            Save settings
          </Button>
        </form>
      </Card>
    </div>
  );
}

function UpdatesCard() {
  const { data, error, busy, reload, setData } = useLoad(() =>
    get<UpgradeStatus>("/api/admin/upgrade"),
  );
  const [starting, setStarting] = useState(false);

  useEffect(() => {
    if (data?.state !== "running") return;
    const id = window.setInterval(reload, 2500);
    return () => window.clearInterval(id);
  }, [data?.state, reload]);

  async function startUpgrade() {
    setStarting(true);
    try {
      const res = await post<UpgradeStatus>("/api/admin/upgrade");
      setData(res);
      toast("Upgrade started");
      reload();
    } catch (err: any) {
      toast(err.message, "error");
    } finally {
      setStarting(false);
    }
  }

  const state = data?.state ?? "idle";
  const running = state === "running";
  const version = data?.current_version || "unknown";
  const commit = data?.current_commit && data.current_commit !== "unknown" ? ` (${data.current_commit})` : "";

  return (
    <Card title="Updates">
      <div className="max-w-2xl space-y-4">
        {error && <ErrorNote>{error}</ErrorNote>}
        {busy && !data ? (
          <Spinner />
        ) : (
          <>
            <div className="grid gap-3 sm:grid-cols-2">
              <div className="rounded-lg border border-line bg-inset px-3 py-2.5">
                <div className="text-[12px] text-faint">Current version</div>
                <div className="mt-1 truncate font-mono text-[13px] text-ink">
                  {version}
                  {commit}
                </div>
              </div>
              <div className="rounded-lg border border-line bg-inset px-3 py-2.5">
                <div className="text-[12px] text-faint">Upgrade status</div>
                <div className={upgradeStateClass(state)}>{upgradeStateText(data)}</div>
              </div>
            </div>

            {data?.message && (
              <div className="rounded-lg border border-line bg-inset px-3 py-2.5 text-[12.5px] leading-relaxed text-muted">
                {data.message}
              </div>
            )}

            <div className="flex flex-wrap gap-2">
              <Button
                type="button"
                variant={data?.available ? "primary" : "outline"}
                onClick={startUpgrade}
                busy={starting}
                disabled={!data?.available || running}
              >
                Upgrade to latest
              </Button>
              <Button type="button" onClick={reload} disabled={starting}>
                Refresh status
              </Button>
            </div>

            {data?.log_tail && data.log_tail.length > 0 && (
              <pre className="max-h-56 overflow-auto rounded-lg border border-line bg-bg-deep p-3 font-mono text-[12px] leading-relaxed text-muted">
                {data.log_tail.join("\n")}
              </pre>
            )}
          </>
        )}
      </div>
    </Card>
  );
}

function upgradeStateText(status?: UpgradeStatus) {
  if (!status) return "Unknown";
  if (status.state === "running" && status.target_version) return `Installing ${status.target_version}`;
  if (status.state === "succeeded" && status.target_version) return `Upgraded to ${status.target_version}`;
  if (status.state === "failed") return "Failed";
  if (status.state === "running") return "Running";
  return "Ready";
}

function upgradeStateClass(state: UpgradeStatus["state"]) {
  const tone =
    state === "failed"
      ? "text-danger"
      : state === "succeeded"
        ? "text-ok"
        : state === "running"
          ? "text-info"
          : "text-ink";
  return `mt-1 text-[13px] font-medium ${tone}`;
}
