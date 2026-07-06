package admin

import (
	"net/http"
	"strconv"

	"github.com/xiqi/wispbox/internal/api/httpjson"
	"github.com/xiqi/wispbox/internal/db"
)

// WriteDNSRecords parses the {domainID} path value, computes the domain's DNS
// records (optionally checking them against live DNS), and writes the standard
// {domain, records} response. It returns the domain (for audit logging) and
// ok=false when it has already written an error. Shared by the admin and setup
// DNS handlers.
func WriteDNSRecords(w http.ResponseWriter, r *http.Request, core *Core, check bool) (*db.Domain, bool) {
	id, _ := strconv.ParseInt(r.PathValue("domainID"), 10, 64)
	dom, records, err := core.DNSRecords(r.Context(), id, check)
	if err != nil {
		httpjson.Fail(w, err)
		return nil, false
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"domain": dom, "records": records})
	return dom, true
}

// SendTestEmail decodes {to}, validates it, and sends a test message from
// postmaster@<primary_hostname> via mailer, writing the {ok[,error]} response.
// It returns the validated recipient (for audit logging) and ok=false when it
// has already written an error. Shared by the admin and setup test-email
// handlers, which supply their own subject and body.
func SendTestEmail(w http.ResponseWriter, r *http.Request, store *db.Store, mailer TestMailer, subject, body string) (to string, ok bool) {
	var req struct {
		To string `json:"to"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return "", false
	}
	if err := db.ValidateEmail(req.To); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return "", false
	}
	primary := store.GetSettingDefault(r.Context(), "primary_hostname", "localhost")
	if err := mailer.SendTest(r.Context(), "postmaster@"+primary, req.To, subject, body); err != nil {
		httpjson.Write(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return req.To, true
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"ok": true})
	return req.To, true
}
