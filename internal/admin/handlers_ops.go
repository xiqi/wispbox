package admin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/xiqi/wispbox/internal/api/httpjson"
	"github.com/xiqi/wispbox/internal/branding"
	"github.com/xiqi/wispbox/internal/buildinfo"
	"github.com/xiqi/wispbox/internal/config"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/services"
	"github.com/xiqi/wispbox/internal/updater"
)

// ---- overview ----

func (h *Handlers) overview(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	ctx := r.Context()

	serviceStatus := map[string]bool{}
	for _, svc := range []string{"postfix", "dovecot", "wispboxd"} {
		active, _ := h.Services.IsActive(ctx, svc)
		serviceStatus[svc] = active
	}

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	disk := map[string]any{}
	var st unix.Statfs_t
	if err := unix.Statfs(h.Core.Cfg.DataDir, &st); err == nil {
		total := uint64(st.Bsize) * st.Blocks
		free := uint64(st.Bsize) * st.Bavail
		disk["total_bytes"] = total
		disk["free_bytes"] = free
		disk["used_bytes"] = total - free
	}

	queueCount := 0
	if n, err := h.Queue.Count(ctx); err == nil {
		queueCount = n
	}

	domains, _ := h.Core.Store.ListDomains(ctx)
	domainViews := []domainView{}
	for _, d := range domains {
		domainViews = append(domainViews, h.domainView(r, d))
	}

	certs, _ := h.Core.Store.ListCertificates(ctx)
	if certs == nil {
		certs = []db.Certificate{}
	}
	deliveryErrors, _ := h.Core.Store.RecentServiceErrors(ctx, 10)
	if deliveryErrors == nil {
		deliveryErrors = []db.ServiceEvent{}
	}
	mailboxes, _ := h.Core.Store.ListMailboxes(ctx, 0)

	httpjson.Write(w, http.StatusOK, map[string]any{
		"mode":           string(h.Core.Cfg.Mode),
		"uptime_seconds": int(time.Since(h.StartedAt).Seconds()),
		"services":       serviceStatus,
		"process_memory": map[string]any{"heap_bytes": mem.HeapAlloc, "sys_bytes": mem.Sys},
		"system_memory":  systemMemory(),
		"disk":           disk,
		"queue_count":    queueCount,
		"domains":        domainViews,
		"mailbox_count":  len(mailboxes),
		"certificates":   certs,
		"recent_errors":  deliveryErrors,
	})
}

// ---- DNS ----

func (h *Handlers) dnsRecords(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	WriteDNSRecords(w, r, h.Core, false)
}

func (h *Handlers) dnsCheck(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	if dom, ok := WriteDNSRecords(w, r, h.Core, true); ok {
		h.auditLog(r, ac, "dns_check", "domain", dom.Name)
	}
}

// ---- certificates ----

func (h *Handlers) listCertificates(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	certs, err := h.Core.Store.ListCertificates(r.Context())
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	if certs == nil {
		certs = []db.Certificate{}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"certificates": certs})
}

// renewCertificate kicks issuance in the background; the UI polls status.
func (h *Handlers) renewCertificate(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	cert, err := h.Core.Store.GetCertificate(r.Context(), id)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "cert_renew", "certificate", cert.Hostname)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_ = h.Core.Certs.IssueNow(ctx, id)
	}()
	httpjson.Write(w, http.StatusAccepted, map[string]any{"status": "renewal started"})
}

// ---- queue ----

func (h *Handlers) queue(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	items, err := h.Queue.List(r.Context())
	if err != nil {
		httpjson.Error(w, http.StatusBadGateway, fmt.Errorf("could not read the mail queue: %v", err))
		return
	}
	if items == nil {
		items = []services.QueueItem{}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"items": items})
}

func (h *Handlers) queueFlush(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	if err := h.Queue.Flush(r.Context()); err != nil {
		httpjson.Error(w, http.StatusBadGateway, err)
		return
	}
	h.auditLog(r, ac, "queue_flush", "queue", "")
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) queueRetry(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id := r.PathValue("id")
	if err := h.Queue.Retry(r.Context(), id); err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "queue_retry", "message", id)
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) queueDelete(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id := r.PathValue("id")
	if err := h.Queue.DeleteMessage(r.Context(), id); err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "queue_delete", "message", id)
	httpjson.Write(w, http.StatusOK, nil)
}

// ---- logs & audit ----

func (h *Handlers) logs(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	n, _ := strconv.Atoi(r.URL.Query().Get("n"))
	var svcs []string
	if s := strings.TrimSpace(r.URL.Query().Get("service")); s != "" {
		svcs = strings.Split(s, ",")
	}
	lines, err := h.Logs.Tail(r.Context(), svcs, n)
	if err != nil {
		httpjson.Error(w, http.StatusBadGateway, fmt.Errorf("could not read logs: %v", err))
		return
	}
	if lines == nil {
		lines = []services.LogLine{}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"lines": lines})
}

func (h *Handlers) audit(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	entries, err := h.Core.Store.RecentAudit(r.Context(), 200)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	if entries == nil {
		entries = []db.AuditLog{}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"entries": entries})
}

// ---- settings ----

// settableKeys are the settings PATCHable through the API.
var settableKeys = map[string]bool{
	"acme_email":         true,
	"server_ipv4":        true,
	"server_ipv6":        true,
	branding.SettingName: true,
}

func (h *Handlers) getSettings(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	all, err := h.Core.Store.AllSettings(r.Context())
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"settings": all})
}

func (h *Handlers) patchSettings(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	var req map[string]string
	if err := httpjson.Decode(w, r, &req, 16<<10); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	// Validate every key and value BEFORE writing any, so a bad value in a
	// multi-field save never leaves settings half-applied.
	clean := map[string]string{}
	for k, v := range req {
		if !settableKeys[k] {
			httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("setting %q cannot be changed here", k))
			return
		}
		v = strings.TrimSpace(v)
		if err := validateSetting(k, v); err != nil {
			httpjson.Error(w, http.StatusBadRequest, err)
			return
		}
		clean[k] = v
	}
	for k, v := range clean {
		if err := h.Core.Store.SetSetting(r.Context(), k, v); err != nil {
			httpjson.Fail(w, err)
			return
		}
		h.auditLog(r, ac, "setting_update", "setting", k)
	}
	all, _ := h.Core.Store.AllSettings(r.Context())
	httpjson.Write(w, http.StatusOK, map[string]any{"settings": all})
}

// validateSetting enforces value shape for the admin-settable keys.
func validateSetting(key, value string) error {
	if value == "" {
		return nil // clearing a value is allowed
	}
	switch key {
	case "acme_email":
		return db.ValidateEmail(value)
	case "server_ipv4":
		return db.ValidateIPv4(value)
	case "server_ipv6":
		return db.ValidateIPv6(value)
	case branding.SettingName:
		return branding.ValidateName(value)
	}
	return nil
}

func (h *Handlers) uploadLogo(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	r.Body = http.MaxBytesReader(w, r.Body, branding.MaxLogoBytes+(64<<10))
	file, _, err := r.FormFile("logo")
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("logo file is required"))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, branding.MaxLogoBytes+1))
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("could not read logo"))
		return
	}
	dataURL, err := branding.LogoDataURL(data)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if err := h.Core.Store.SetSetting(r.Context(), branding.SettingLogo, dataURL); err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "setting_update", "setting", branding.SettingLogo)
	all, _ := h.Core.Store.AllSettings(r.Context())
	httpjson.Write(w, http.StatusOK, map[string]any{"settings": all})
}

func (h *Handlers) deleteLogo(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	if err := h.Core.Store.SetSetting(r.Context(), branding.SettingLogo, ""); err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "setting_update", "setting", branding.SettingLogo)
	all, _ := h.Core.Store.AllSettings(r.Context())
	httpjson.Write(w, http.StatusOK, map[string]any{"settings": all})
}

// ---- one-click upgrade ----

func (h *Handlers) upgradeStatus(w http.ResponseWriter, r *http.Request, _ *adminCtx) {
	httpjson.Write(w, http.StatusOK, h.currentUpgradeStatus(r.Context()))
}

func (h *Handlers) startUpgrade(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	if h.Core.Cfg.IsDev() {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("one-click upgrades are available on installed systemd servers"))
		return
	}
	st := h.currentUpgradeStatus(r.Context())
	if st.State == "running" {
		httpjson.Error(w, http.StatusConflict, fmt.Errorf("upgrade already running"))
		return
	}

	cmd := exec.CommandContext(r.Context(), "sudo", "-n", "/usr/bin/systemctl", "start", "--no-block", "wispbox-upgrade.service")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		httpjson.Error(w, http.StatusInternalServerError, fmt.Errorf("could not start upgrade: %s", msg))
		return
	}
	h.auditLog(r, ac, "upgrade_start", "system", "latest")
	st = h.currentUpgradeStatus(r.Context())
	if st.State != "running" {
		st.State = "running"
		st.Message = "Upgrade queued"
	}
	httpjson.Write(w, http.StatusAccepted, st)
}

func (h *Handlers) currentUpgradeStatus(ctx context.Context) updater.Status {
	st := updater.Read(ctx, h.Core.Cfg.DataDir, h.Core.Cfg.LogDir, 80)
	st.Available = h.Core.Cfg.Mode == config.ModeProduction
	st.CurrentVersion = buildinfo.Version
	st.CurrentCommit = buildinfo.Commit
	st.CurrentDate = buildinfo.Date
	if !st.Available && st.Message == "" {
		st.Message = "One-click upgrades run only on installed systemd servers."
	}
	return st
}
