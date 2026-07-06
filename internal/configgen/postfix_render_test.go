package configgen_test

import (
	"context"
	"strings"
	"testing"

	"github.com/xiqi/wispbox/internal/configgen"
)

// hasLine reports whether content contains line exactly (ignoring the header).
func hasLine(content []byte, line string) bool {
	for _, l := range strings.Split(string(content), "\n") {
		if l == line {
			return true
		}
	}
	return false
}

// keyLines returns the first field of every non-comment line.
func keyLines(content []byte) []string {
	var keys []string
	for _, l := range strings.Split(string(content), "\n") {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		keys = append(keys, strings.Fields(l)[0])
	}
	return keys
}

func TestRenderMaps(t *testing.T) {
	builder := newTestEnv(t).builder
	data, err := builder.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files, err := configgen.RenderPostfix(data)
	if err != nil {
		t.Fatalf("RenderPostfix: %v", err)
	}

	wantLines := []struct{ file, line string }{
		{"virtual_domains", "example.com OK"},
		{"virtual_domains", "startup.dev OK"},
		{"virtual_mailboxes", "alice@example.com example.com/alice/Maildir/"},
		{"virtual_aliases", "hello@example.com alice@example.com"},
		{"virtual_aliases", "@example.com alice@example.com"},
		{"sender_logins", "alice@example.com alice@example.com"},
		{"sender_logins", "hello@example.com alice@example.com"},
		{"sender_transport", "@startup.dev smtp:[smtp.starttls.test]:587"},
		{"sasl_passwd", "[smtp.starttls.test]:587 startup-user:starttls-pass"},
		{"sasl_passwd", "[smtp.implicit.test]:465 implicit-user:implicit-pass"},
		{"tls_policy", "[smtp.starttls.test]:587 encrypt"},
	}
	for _, w := range wantLines {
		content, ok := files[w.file]
		if !ok {
			t.Fatalf("rendered output is missing %s", w.file)
		}
		if !hasLine(content, w.line) {
			t.Errorf("%s: missing line %q; got:\n%s", w.file, w.line, content)
		}
	}

	// The catch-all receives mail but never grants send rights.
	for _, key := range keyLines(files["sender_logins"]) {
		if key == "@example.com" {
			t.Errorf("sender_logins grants the catch-all @example.com send rights:\n%s", files["sender_logins"])
		}
	}
	// Implicit-TLS relays use the relaytls transport (wrappermode), not a
	// STARTTLS "encrypt" policy entry.
	for _, key := range keyLines(files["tls_policy"]) {
		if key == "[smtp.implicit.test]:465" {
			t.Errorf("tls_policy has an entry for the implicit-TLS relay:\n%s", files["tls_policy"])
		}
	}
}

func TestRenderMainCF(t *testing.T) {
	builder := newTestEnv(t).builder
	data, err := builder.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files, err := configgen.RenderPostfix(data)
	if err != nil {
		t.Fatalf("RenderPostfix: %v", err)
	}
	main := string(files["main.cf"])

	wants := []string{
		"myhostname = mail.example.com",
		"virtual_mailbox_domains = texthash:" + data.PostfixMapDir + "/virtual_domains",
		"virtual_mailbox_maps = texthash:" + data.PostfixMapDir + "/virtual_mailboxes",
		"virtual_alias_maps = texthash:" + data.PostfixMapDir + "/virtual_aliases",
		"smtpd_sender_login_maps = texthash:" + data.PostfixMapDir + "/sender_logins",
		"sender_dependent_default_transport_maps = texthash:" + data.PostfixMapDir + "/sender_transport",
		"smtp_sasl_password_maps = texthash:" + data.PostfixMapDir + "/sasl_passwd",
		"smtp_tls_policy_maps = texthash:" + data.PostfixMapDir + "/tls_policy",
		"tls_server_sni_maps = texthash:" + data.PostfixMapDir + "/sni_maps",
		// The global policy relays through the implicit-TLS endpoint.
		"default_transport = relaytls:[smtp.implicit.test]:465",
	}
	for _, w := range wants {
		if !strings.Contains(main, w) {
			t.Errorf("main.cf: missing %q", w)
		}
	}
}

func TestRenderMasterCFRequiredZeroProcessLimits(t *testing.T) {
	builder := newTestEnv(t).builder
	data, err := builder.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files, err := configgen.RenderPostfix(data)
	if err != nil {
		t.Fatalf("RenderPostfix: %v", err)
	}
	master := string(files["master.cf"])

	for _, want := range []string{
		"smtp      inet  n       -       y       -       8       smtpd",
		"submission inet n       -       y       -       6       smtpd",
		"cleanup   unix  n       -       y       -       0       cleanup",
		"flush     unix  n       -       y       1000?   0       flush",
	} {
		if !strings.Contains(master, want) {
			t.Errorf("master.cf: missing %q; got:\n%s", want, master)
		}
	}
}

func TestRenderLeavesNoTemplateArtifacts(t *testing.T) {
	builder := newTestEnv(t).builder
	data, err := builder.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	files, err := configgen.RenderPostfix(data)
	if err != nil {
		t.Fatalf("RenderPostfix: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("RenderPostfix produced no files")
	}
	for name, content := range files {
		if strings.Contains(string(content), "{{") || strings.Contains(string(content), "}}") {
			t.Errorf("%s contains unrendered template artifacts:\n%s", name, content)
		}
	}
}
