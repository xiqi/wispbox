import { type ChangeEvent, type FormEvent, useEffect, useMemo, useRef, useState } from "react";
import { RefreshCw } from "lucide-react";
import { del, get, patch, post, postForm } from "../../../lib/api";
import { brandSettingKeys, useBrand } from "../../../lib/brand";
import { useLoad } from "../../../lib/hooks";
import type { Domain, UpgradeStatus } from "../../../lib/types";
import {
  Button,
  Card,
  ErrorNote,
  Field,
  Input,
  Select,
  Spinner,
  toast,
  WispMark,
} from "../../../components/ui";

export default function Settings() {
  const brand = useBrand();
  const { data, error, busy, reload } = useLoad(() =>
    get<{ settings: Record<string, string> }>("/api/admin/settings"),
  );
  const domains = useLoad(() => get<{ domains: Domain[] }>("/api/admin/domains"));
  const logoInputRef = useRef<HTMLInputElement | null>(null);
  const [acmeEmail, setAcmeEmail] = useState("");
  const [ipv4, setIpv4] = useState("");
  const [ipv6, setIpv6] = useState("");
  const [appearanceDomain, setAppearanceDomain] = useState("");
  const [brandName, setBrandName] = useState("");
  const [logo, setLogo] = useState("");
  const [saving, setSaving] = useState(false);
  const [savingBrand, setSavingBrand] = useState(false);
  const [uploadingLogo, setUploadingLogo] = useState(false);
  const currentBrandKeys = useMemo(() => brandSettingKeys(appearanceDomain), [appearanceDomain]);

  useEffect(() => {
    if (data) {
      setAcmeEmail(data.settings.acme_email ?? "");
      setIpv4(data.settings.server_ipv4 ?? "");
      setIpv6(data.settings.server_ipv6 ?? "");
    }
  }, [data]);

  useEffect(() => {
    if (!data) return;
    setBrandName(data.settings[currentBrandKeys.name] ?? "");
    setLogo(data.settings[currentBrandKeys.logo] ?? "");
  }, [data, currentBrandKeys.name, currentBrandKeys.logo]);

  if ((busy && !data) || (domains.busy && !domains.data)) return <Spinner />;
  if (error) return <ErrorNote>{error}</ErrorNote>;
  if (domains.error) return <ErrorNote>{domains.error}</ErrorNote>;

  const domainList = domains.data?.domains ?? [];
  const globalName = data?.settings.brand_name?.trim() ?? "";
  const globalLogo = data?.settings.brand_logo ?? "";
  const previewName = brandName.trim() || (appearanceDomain ? globalName : "") || "wispbox";
  const previewLogo = logo || (appearanceDomain ? globalLogo : "");
  const scopeHint = appearanceDomain ? "Overrides this domain." : "Used when a domain has no override.";
  const nameHint = appearanceDomain ? "Leave empty to use the global default." : "Leave empty to use wispbox.";
  const logoHint = appearanceDomain
    ? "PNG, JPEG, or WebP. 256 KB max. Leave empty to use the global logo."
    : "PNG, JPEG, or WebP. 256 KB max.";
  const logoEndpoint = appearanceDomain
    ? `/api/admin/settings/logo?domain=${encodeURIComponent(appearanceDomain)}`
    : "/api/admin/settings/logo";

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
        [currentBrandKeys.name]: brandName,
      });
      setBrandName(res.settings[currentBrandKeys.name] ?? "");
      setLogo(res.settings[currentBrandKeys.logo] ?? "");
      await brand.reloadBrand().catch(() => undefined);
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
      const res = await postForm<{ settings: Record<string, string> }>(logoEndpoint, form);
      setLogo(res.settings[currentBrandKeys.logo] ?? "");
      await brand.reloadBrand().catch(() => undefined);
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
      const res = await del<{ settings: Record<string, string> }>(logoEndpoint);
      setLogo(res.settings[currentBrandKeys.logo] ?? "");
      await brand.reloadBrand().catch(() => undefined);
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
        <div className="max-w-xl space-y-5">
          <Field label="Scope" hint={scopeHint}>
            <Select value={appearanceDomain} onChange={(e) => setAppearanceDomain(e.target.value)}>
              <option value="">Global default</option>
              {domainList.map((domain) => (
                <option key={domain.id} value={domain.name}>
                  {domain.name}
                </option>
              ))}
            </Select>
          </Field>

          <div className="flex items-center gap-4">
            <div className="flex h-16 w-24 items-center justify-center rounded-lg border border-line bg-inset px-3">
              {previewLogo ? (
                <img
                  src={previewLogo}
                  alt=""
                  aria-hidden
                  className="shrink-0 object-contain"
                  style={{ width: 34 * 1.66, height: 34 }}
                />
              ) : (
                <WispMark size={34} />
              )}
            </div>
            <div className="min-w-0">
              <div className="truncate text-[13.5px] font-medium text-ink">{previewName}</div>
              <div className="mt-1 text-[12.5px] leading-relaxed text-muted">
                {appearanceDomain || "Global default"}
              </div>
            </div>
          </div>

          <form onSubmit={saveBrand} className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-start">
            <Field label="Display name" hint={nameHint}>
              <Input
                value={brandName}
                maxLength={40}
                onChange={(e) => setBrandName(e.target.value)}
                placeholder={appearanceDomain ? globalName || "wispbox" : "wispbox"}
              />
            </Field>
            <Button type="submit" variant="primary" busy={savingBrand} className="w-full sm:mt-6 sm:w-auto">
              Save appearance
            </Button>
          </form>

          <div className="space-y-3 border-t border-line pt-5">
            <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-start">
              <Field label="Logo" hint={logoHint}>
                <input
                  ref={logoInputRef}
                  type="file"
                  accept="image/png,image/jpeg,image/webp"
                  disabled={uploadingLogo}
                  onChange={uploadLogo}
                  className="block w-full text-[13px] text-muted file:mr-3 file:h-8 file:rounded-lg file:border file:border-line-strong file:bg-inset file:px-3 file:text-[12.5px] file:font-medium file:text-ink hover:file:bg-raised disabled:opacity-50"
                />
              </Field>
              <Button
                type="button"
                onClick={removeLogo}
                disabled={!logo || uploadingLogo}
                busy={uploadingLogo}
                className="w-full sm:mt-6 sm:w-auto"
              >
                Remove logo
              </Button>
            </div>
          </div>
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
  const [refreshing, setRefreshing] = useState(false);

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

  function refreshStatus() {
    if (refreshing) return;
    setRefreshing(true);
    reload();
    window.setTimeout(() => setRefreshing(false), 500);
  }

  const state = data?.state ?? "idle";
  const running = state === "running";
  const version = data?.current_version || "unknown";
  const latest = data?.latest_version || "unknown";
  const updateAvailable = data?.update_available === true;
  const canUpgrade = data?.available === true && updateAvailable && !running;
  const refreshBusy = refreshing || (busy && Boolean(data));
  const upgradeLabel =
    data?.available === false ? "Unavailable" : updateAvailable ? "Upgrade to latest" : "Up to date";
  const commit = data?.current_commit && data.current_commit !== "unknown" ? ` (${data.current_commit})` : "";
  const refreshAction = (
    <Button type="button" size="sm" onClick={refreshStatus} busy={refreshBusy}>
      <RefreshCw size={13} className={refreshBusy ? "animate-spin" : ""} />
      Refresh
    </Button>
  );

  return (
    <Card title="Updates" actions={refreshAction}>
      <div className="max-w-2xl space-y-4">
        {error && <ErrorNote>{error}</ErrorNote>}
        {busy && !data ? (
          <Spinner />
        ) : (
          <>
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="rounded-lg border border-line bg-inset px-3 py-2.5">
                <div className="text-[12px] text-faint">Current version</div>
                <div className="mt-1 truncate font-mono text-[13px] text-ink">
                  {version}
                  {commit}
                </div>
              </div>
              <div className="rounded-lg border border-line bg-inset px-3 py-2.5">
                <div className="text-[12px] text-faint">Latest version</div>
                <div className="mt-1 truncate font-mono text-[13px] text-ink">{latest}</div>
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
                variant={canUpgrade ? "primary" : "outline"}
                onClick={startUpgrade}
                busy={starting}
                disabled={!canUpgrade}
              >
                {upgradeLabel}
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
  if (status.state === "failed") return "Failed";
  if (status.state === "running") return "Running";
  if (status.latest_version && !status.update_available) return "Up to date";
  if (status.state === "succeeded" && status.target_version) return `Upgraded to ${status.target_version}`;
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
