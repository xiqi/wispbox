package configgen_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiqi/wispbox/internal/config"
	"github.com/xiqi/wispbox/internal/configgen"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/delivery"
	"github.com/xiqi/wispbox/internal/security"
	"github.com/xiqi/wispbox/internal/services"
)

type testEnv struct {
	cfg        *config.Config
	store      *db.Store
	builder    *configgen.Builder
	domExample *db.Domain
	domStartup *db.Domain
}

// newTestEnv builds a real store in a temp dir with the standard fixture:
// example.com (mailbox alice, alias hello@, catch-all @example.com),
// startup.dev routed through a STARTTLS relay on 587, and a global
// implicit-TLS relay on 465.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()

	cfg := config.DevelopmentDefaults(t.TempDir())
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	sqldb, err := db.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { sqldb.Close() })
	if _, err := db.Migrate(ctx, sqldb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := db.NewStore(sqldb)

	secret, err := security.LoadOrCreateSecret(cfg.SecretPath)
	if err != nil {
		t.Fatalf("load secret: %v", err)
	}
	if err := store.SetSetting(ctx, "primary_hostname", "mail.example.com"); err != nil {
		t.Fatalf("set primary_hostname: %v", err)
	}

	domExample, err := store.CreateDomain(ctx, "example.com", "")
	if err != nil {
		t.Fatalf("create example.com: %v", err)
	}
	domStartup, err := store.CreateDomain(ctx, "startup.dev", "")
	if err != nil {
		t.Fatalf("create startup.dev: %v", err)
	}
	if _, err := store.CreateMailbox(ctx, domExample.ID, "alice", "$2y$10$notarealhash", 1024); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}
	if _, err := store.CreateAlias(ctx, domExample.ID, "hello", "alice@example.com", false); err != nil {
		t.Fatalf("create alias: %v", err)
	}
	if _, err := store.CreateAlias(ctx, domExample.ID, "", "alice@example.com", true); err != nil {
		t.Fatalf("create catch-all: %v", err)
	}

	starttlsPass, err := security.Encrypt(secret, "starttls-pass")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	starttlsRelay, err := store.CreateRelay(ctx, db.OutboundRelay{
		Name: "startup-relay", Provider: "custom", Host: "smtp.starttls.test", Port: 587,
		Username: "startup-user", EncryptedPassword: starttlsPass,
		TLSMode: db.TLSModeStartTLS, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create starttls relay: %v", err)
	}
	implicitPass, err := security.Encrypt(secret, "implicit-pass")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	implicitRelay, err := store.CreateRelay(ctx, db.OutboundRelay{
		Name: "global-relay", Provider: "resend", Host: "smtp.implicit.test", Port: 465,
		Username: "implicit-user", EncryptedPassword: implicitPass,
		TLSMode: db.TLSModeTLS, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create implicit relay: %v", err)
	}

	engine := delivery.NewEngine(store)
	if _, err := engine.SetPolicy(ctx, db.ScopeGlobal, 0, db.ModeRelay, &implicitRelay.ID); err != nil {
		t.Fatalf("set global policy: %v", err)
	}
	if _, err := engine.SetPolicy(ctx, db.ScopeDomain, domStartup.ID, db.ModeRelay, &starttlsRelay.ID); err != nil {
		t.Fatalf("set startup.dev policy: %v", err)
	}

	return &testEnv{
		cfg:   cfg,
		store: store,
		builder: &configgen.Builder{
			Cfg: cfg, Store: store, Engine: engine, Secret: secret,
			MailUser: "wispbox-dev", MailGroup: "wispbox-dev",
		},
		domExample: domExample,
		domStartup: domStartup,
	}
}

func newGenerator(env *testEnv) *configgen.Generator {
	return &configgen.Generator{
		Builder:        env.builder,
		Services:       services.NewMockManager(env.store),
		Store:          env.store,
		ReloadServices: false,
	}
}

func TestEnsureDKIMKey(t *testing.T) {
	dir := t.TempDir()

	txt, err := configgen.EnsureDKIMKey(dir, "example.com")
	if err != nil {
		t.Fatalf("EnsureDKIMKey: %v", err)
	}
	const prefix = "v=DKIM1; k=rsa; p="
	if !strings.HasPrefix(txt, prefix) {
		t.Fatalf("TXT value %q does not start with %q", txt, prefix)
	}
	if strings.TrimPrefix(txt, prefix) == "" {
		t.Fatal("TXT value has an empty public key")
	}

	keyPath := filepath.Join(dir, "example.com", configgen.DKIMSelector+".private")
	fi, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o640 {
		t.Errorf("key mode = %04o, want 0640", got)
	}
	keyBefore, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}

	// A second call must reuse the existing key, not rotate it.
	txt2, err := configgen.EnsureDKIMKey(dir, "example.com")
	if err != nil {
		t.Fatalf("EnsureDKIMKey (second call): %v", err)
	}
	if txt2 != txt {
		t.Errorf("TXT value changed across calls:\nfirst:  %s\nsecond: %s", txt, txt2)
	}
	keyAfter, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}
	if !bytes.Equal(keyBefore, keyAfter) {
		t.Error("private key was rewritten by the second EnsureDKIMKey call")
	}
}

func TestGeneratorApply(t *testing.T) {
	env := newTestEnv(t)
	gen := newGenerator(env)

	if err := gen.Apply(context.Background()); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	checks := []struct{ path, want string }{
		{"postfix/main.cf", "myhostname = mail.example.com"},
		{"postfix/main.cf", "default_transport = relaytls:[smtp.implicit.test]:465"},
		{"postfix/master.cf", "relaytls"},
		{"postfix/virtual_domains", "example.com OK"},
		{"postfix/virtual_domains", "startup.dev OK"},
		{"postfix/virtual_mailboxes", "alice@example.com example.com/alice/Maildir/"},
		{"postfix/virtual_aliases", "@example.com alice@example.com"},
		{"postfix/sender_logins", "hello@example.com alice@example.com"},
		{"postfix/sender_transport", "@startup.dev smtp:[smtp.starttls.test]:587"},
		{"postfix/sasl_passwd", "[smtp.starttls.test]:587 startup-user:starttls-pass"},
		{"postfix/sasl_passwd", "[smtp.implicit.test]:465 implicit-user:implicit-pass"},
		{"postfix/tls_policy", "[smtp.starttls.test]:587 encrypt"},
		{"postfix/sni_maps", "Generated by wispbox"},
		{"dovecot/dovecot.conf", "mail_location = maildir:" + env.cfg.MailDir + "/%d/%n/Maildir"},
		{"dovecot/dovecot-sql.conf.ext", "default_pass_scheme = BLF-CRYPT"},
		{"opendkim/opendkim.conf", "KeyTable"},
		{"opendkim/key.table", "Generated by wispbox"},
		{"opendkim/signing.table", "Generated by wispbox"},
	}
	for _, c := range checks {
		full := filepath.Join(env.cfg.GeneratedDir, c.path)
		content, err := os.ReadFile(full)
		if err != nil {
			t.Errorf("expected generated file %s: %v", c.path, err)
			continue
		}
		if !strings.Contains(string(content), c.want) {
			t.Errorf("%s: missing %q; got:\n%s", c.path, c.want, content)
		}
	}

	// sasl_passwd holds relay credentials and must be private.
	fi, err := os.Stat(filepath.Join(env.cfg.GeneratedDir, "postfix", "sasl_passwd"))
	if err != nil {
		t.Fatalf("stat sasl_passwd: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("sasl_passwd mode = %04o, want 0600", got)
	}
	fi, err = os.Stat(filepath.Join(env.cfg.GeneratedDir, "postfix", "main.cf"))
	if err != nil {
		t.Fatalf("stat main.cf: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o640 {
		t.Errorf("main.cf mode = %04o, want 0640", got)
	}
}

// snapshotDir reads every regular file under dir, keyed by relative path.
func snapshotDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = content
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	return out
}

func TestApplyKeepsLastGoodConfigOnFailure(t *testing.T) {
	env := newTestEnv(t)
	gen := newGenerator(env)
	ctx := context.Background()

	if err := gen.Apply(ctx); err != nil {
		t.Fatalf("initial Apply: %v", err)
	}
	before := snapshotDir(t, env.cfg.GeneratedDir)
	if len(before) == 0 {
		t.Fatal("initial Apply generated no files")
	}

	// Break the delivery data behind the store's back: deleting the relays
	// leaves both relay-mode policies without a relay (the FK is ON DELETE
	// SET NULL), which Build must refuse to render.
	if _, err := env.store.DB().ExecContext(ctx, `DELETE FROM outbound_relays`); err != nil {
		t.Fatalf("break relays: %v", err)
	}

	if err := gen.Apply(ctx); err == nil {
		t.Fatal("Apply succeeded with a relay policy pointing at no relay; want error")
	}

	after := snapshotDir(t, env.cfg.GeneratedDir)
	if len(after) != len(before) {
		t.Fatalf("generated file set changed after failed Apply: %d files before, %d after", len(before), len(after))
	}
	for rel, want := range before {
		got, ok := after[rel]
		if !ok {
			t.Errorf("%s disappeared after failed Apply", rel)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s changed after failed Apply", rel)
		}
	}
}

func TestApplyReturnsReloadFailures(t *testing.T) {
	env := newTestEnv(t)
	mock := services.NewMockManager(env.store)
	mock.FailOn["postfix"] = true
	gen := &configgen.Generator{
		Builder:        env.builder,
		Services:       mock,
		Store:          env.store,
		ReloadServices: true,
	}

	err := gen.Apply(context.Background())
	if err == nil {
		t.Fatal("Apply succeeded with a failed service reload; want error")
	}
	if !strings.Contains(err.Error(), "reload generated configuration") {
		t.Fatalf("Apply error = %v, want reload context", err)
	}
}

// TestNexthopCredentialCollision covers the review fix: two relays that share
// a host:port but carry different credentials must fail loudly rather than
// silently authenticating one with the other's login.
func TestNexthopCredentialCollision(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)
	secret, _ := security.LoadOrCreateSecret(env.cfg.SecretPath)

	// A second domain routed through a relay on the SAME host:port as the
	// global relay (smtp.implicit.test:465) but with a different login.
	dom, err := env.store.CreateDomain(ctx, "clash.example", "")
	if err != nil {
		t.Fatal(err)
	}
	pass, _ := security.Encrypt(secret, "other-pass")
	clashRelay, err := env.store.CreateRelay(ctx, db.OutboundRelay{
		Name: "clash-relay", Provider: "resend", Host: "smtp.implicit.test", Port: 465,
		Username: "different-user", EncryptedPassword: pass, TLSMode: db.TLSModeTLS, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	engine := delivery.NewEngine(env.store)
	if _, err := engine.SetPolicy(ctx, db.ScopeDomain, dom.ID, db.ModeRelay, &clashRelay.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := env.builder.Build(ctx); err == nil {
		t.Fatal("expected Build to reject conflicting credentials for the same nexthop")
	} else if !strings.Contains(err.Error(), "different credentials") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestCredentiallessRelayEmitsNoSASL covers the review fix: a relay with no
// username/password must not produce a ":" sasl_passwd line (which would make
// Postfix send an empty AUTH) and must not break config generation.
func TestCredentiallessRelayEmitsNoSASL(t *testing.T) {
	ctx := context.Background()
	env := newTestEnv(t)

	// Point the global policy at a brand-new credential-less relay.
	relay, err := env.store.CreateRelay(ctx, db.OutboundRelay{
		Name: "open-relay-host", Provider: "custom", Host: "smtp.nocreds.test", Port: 587,
		Username: "", EncryptedPassword: "", TLSMode: db.TLSModeStartTLS, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	engine := delivery.NewEngine(env.store)
	if _, err := engine.SetPolicy(ctx, db.ScopeGlobal, 0, db.ModeRelay, &relay.ID); err != nil {
		t.Fatal(err)
	}

	data, err := env.builder.Build(ctx)
	if err != nil {
		t.Fatalf("Build should tolerate a credential-less relay: %v", err)
	}
	for _, e := range data.SASLEntries {
		if e.Nexthop == "[smtp.nocreds.test]:587" {
			t.Errorf("credential-less relay emitted a sasl_passwd entry: %q", e.Credentials)
		}
	}
}
