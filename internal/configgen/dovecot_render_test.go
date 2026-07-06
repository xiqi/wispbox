package configgen_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiqi/wispbox/internal/configgen"
)

func writeCertFiles(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	certPath = filepath.Join(dir, "fullchain.pem")
	keyPath = filepath.Join(dir, "privkey.pem")
	for _, p := range []string{certPath, keyPath} {
		if err := os.WriteFile(p, []byte("dummy pem\n"), 0o640); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	return certPath, keyPath
}

func markIssued(t *testing.T, env *testEnv, certID int64, certPath, keyPath string) {
	t.Helper()
	now := time.Now()
	if err := env.store.MarkCertificateIssued(context.Background(), certID, "http-01",
		certPath, keyPath, now, now.AddDate(0, 3, 0), now.AddDate(0, 2, 0)); err != nil {
		t.Fatalf("mark issued: %v", err)
	}
}

func TestRenderDovecotConf(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	// Active certificate with files on disk: gets a local_name block.
	onDisk, err := env.store.CreateCertificate(ctx, env.domExample.ID, "mail.example.com")
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPath, keyPath := writeCertFiles(t, filepath.Join(env.cfg.CertDir, "mail.example.com"))
	markIssued(t, env, onDisk.ID, certPath, keyPath)

	// Active certificate whose files are gone: must be skipped, or Dovecot
	// would refuse to start on a missing ssl_cert file.
	missing, err := env.store.CreateCertificate(ctx, env.domStartup.ID, "mail.startup.dev")
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	markIssued(t, env, missing.ID,
		filepath.Join(env.cfg.CertDir, "mail.startup.dev", "fullchain.pem"),
		filepath.Join(env.cfg.CertDir, "mail.startup.dev", "privkey.pem"))

	// Pending certificate: no paths yet, never rendered.
	if _, err := env.store.CreateCertificate(ctx, env.domExample.ID, "imap.example.com"); err != nil {
		t.Fatalf("create cert: %v", err)
	}

	data, err := env.builder.Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files, err := configgen.RenderDovecot(data)
	if err != nil {
		t.Fatalf("RenderDovecot: %v", err)
	}
	conf := string(files["dovecot.conf"])

	wants := []string{
		"mail_location = maildir:" + env.cfg.MailDir + "/%d/%n/Maildir",
		"local_name mail.example.com {",
		"ssl_cert = <" + certPath,
		"ssl_key = <" + keyPath,
	}
	for _, w := range wants {
		if !strings.Contains(conf, w) {
			t.Errorf("dovecot.conf: missing %q", w)
		}
	}
	for _, unwanted := range []string{
		"local_name mail.startup.dev",
		"local_name imap.example.com",
	} {
		if strings.Contains(conf, unwanted) {
			t.Errorf("dovecot.conf: has %q but the certificate is not usable", unwanted)
		}
	}

	// The SQL auth workers must run as the mail user to reach the database.
	_, worker, found := strings.Cut(conf, "service auth-worker {")
	if !found {
		t.Fatal("dovecot.conf: missing service auth-worker block")
	}
	worker, _, _ = strings.Cut(worker, "}")
	if !strings.Contains(worker, "user = wispbox-dev") {
		t.Errorf("auth-worker block does not set user = wispbox-dev:\n%s", worker)
	}
}

func TestRenderDovecotSQL(t *testing.T) {
	env := newTestEnv(t)
	data, err := env.builder.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files, err := configgen.RenderDovecot(data)
	if err != nil {
		t.Fatalf("RenderDovecot: %v", err)
	}
	sqlConf := string(files["dovecot-sql.conf.ext"])

	for _, w := range []string{
		"default_pass_scheme = BLF-CRYPT",
		"connect = " + env.cfg.DBPath,
		"CASE WHEN m.quota_mb > 0 THEN '*:storage=' || m.quota_mb || 'M' ELSE NULL END AS quota_rule",
	} {
		if !strings.Contains(sqlConf, w) {
			t.Errorf("dovecot-sql.conf.ext: missing %q", w)
		}
	}

	for name, content := range files {
		if strings.Contains(string(content), "{{") || strings.Contains(string(content), "}}") {
			t.Errorf("%s contains unrendered template artifacts:\n%s", name, content)
		}
	}
}

// TestRenderDovecot24 covers the Dovecot 2.4 template path (Debian 13,
// Ubuntu 26.04): the renderer must emit 2.4 syntax and none of the 2.3
// directives that 2.4 removed or renamed.
func TestRenderDovecot24(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	onDisk, err := env.store.CreateCertificate(ctx, env.domExample.ID, "mail.example.com")
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPath, keyPath := writeCertFiles(t, filepath.Join(env.cfg.CertDir, "mail.example.com"))
	markIssued(t, env, onDisk.ID, certPath, keyPath)

	env.builder.DovecotV24 = true
	data, err := env.builder.Build(ctx)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !data.DovecotV24 {
		t.Fatal("DovecotV24 did not propagate from Builder to Data")
	}
	files, err := configgen.RenderDovecot(data)
	if err != nil {
		t.Fatalf("RenderDovecot: %v", err)
	}
	conf := string(files["dovecot.conf"])
	sqlConf := string(files["dovecot-sql.conf.ext"])

	// 2.4 directives that must be present.
	for _, w := range []string{
		"mail_driver = maildir",
		"mail_path = " + env.cfg.MailDir + "/%{user | domain}/%{user | username}/Maildir",
		"ssl_server_cert_file = " + certPath,
		"ssl_server_key_file = " + keyPath,
		"local_name mail.example.com {",
		"auth_allow_cleartext = no",
		// Quota enforcement, consistent with the 2.3 template.
		"mail_plugins {",
		"quota = yes",
		"imap_quota = yes",
		`quota "User quota" {`,
		"driver = count",
	} {
		if !strings.Contains(conf, w) {
			t.Errorf("dovecot.conf (2.4): missing %q", w)
		}
	}
	// 2.3 syntax that 2.4 removed/renamed must be absent.
	for _, bad := range []string{
		"mail_location =",
		"ssl_cert = <",
		"ssl_key = <",
		"disable_plaintext_auth",
		"plugin {",
	} {
		if strings.Contains(conf, bad) {
			t.Errorf("dovecot.conf (2.4): still contains 2.3 syntax %q", bad)
		}
	}

	// 2.4 SQL: inline driver + named passdb/userdb blocks, %{user}, no
	// external connect= line.
	for _, w := range []string{
		"sql_driver = sqlite",
		"sqlite_path = " + env.cfg.DBPath,
		"passdb sql {",
		"userdb sql {",
		"'%{user}'",
		"BLF-CRYPT",
		// Per-user quota limit uses the 2.4 userdb_-prefixed field.
		"userdb_quota_storage_size",
		"CASE WHEN m.quota_mb > 0 THEN m.quota_mb || 'M' ELSE NULL END AS userdb_quota_storage_size",
	} {
		if !strings.Contains(sqlConf, w) {
			t.Errorf("dovecot-sql.conf.ext (2.4): missing %q", w)
		}
	}
	// 2.3-only markers must be gone (connect= line, *_query names, '%u').
	for _, bad := range []string{"connect = ", "password_query", "user_query", "'%u'"} {
		if strings.Contains(sqlConf, bad) {
			t.Errorf("dovecot-sql.conf.ext (2.4): still contains 2.3 syntax %q", bad)
		}
	}

	for name, content := range files {
		if strings.Contains(string(content), "{{") || strings.Contains(string(content), "}}") {
			t.Errorf("%s (2.4) contains unrendered template artifacts", name)
		}
	}
}

// TestRenderDovecotVersionSwitch confirms the same data renders 2.3 by
// default and 2.4 when the flag flips.
func TestRenderDovecotVersionSwitch(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	env.builder.DovecotV24 = false
	d23, _ := env.builder.Build(ctx)
	f23, _ := configgen.RenderDovecot(d23)
	if !strings.Contains(string(f23["dovecot.conf"]), "mail_location =") {
		t.Error("2.3 render should use mail_location")
	}

	env.builder.DovecotV24 = true
	d24, _ := env.builder.Build(ctx)
	f24, _ := configgen.RenderDovecot(d24)
	if !strings.Contains(string(f24["dovecot.conf"]), "mail_driver = maildir") {
		t.Error("2.4 render should use mail_driver")
	}
}
