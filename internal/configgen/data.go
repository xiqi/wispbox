// Package configgen turns the wispbox database into Postfix, Dovecot, and
// OpenDKIM configuration. Rendering is pure (testable without a mail
// server); applying writes atomically and never replaces a working config
// with a broken one.
package configgen

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xiqi/wispbox/internal/config"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/delivery"
	"github.com/xiqi/wispbox/internal/security"
)

// DKIMSelector is the fixed selector wispbox uses for all domains.
const DKIMSelector = "wisp1"

// Data is everything the templates need, precomputed.
type Data struct {
	// Environment.
	PrimaryHostname      string
	PostfixMapDir        string // generated/postfix
	GeneratedDovecotDir  string
	GeneratedOpenDKIMDir string
	MailDir              string
	DBPath               string
	MailUser             string
	MailGroup            string
	MessageSizeLimit     int64

	// TLS.
	DefaultCertPath string
	DefaultKeyPath  string
	SNI             []SNIEntry

	// Mail objects.
	Domains      []db.Domain
	Mailboxes    []db.Mailbox
	Aliases      []db.Alias
	SenderLogins []SenderLogin

	// Outbound routing.
	DefaultTransport string
	SenderTransports []SenderTransport
	SASLEntries      []SASLEntry
	TLSPolicies      []TLSPolicy

	// DKIM.
	DKIMEnabled bool
	DKIMKeys    []DKIMKey

	// DovecotV24 selects the Dovecot 2.4 config templates (Debian 13,
	// Ubuntu 26.04). Older systems (Debian 12, Ubuntu 24.04) use 2.3.
	DovecotV24 bool
}

type SNIEntry struct{ Hostname, CertPath, KeyPath string }

type SenderLogin struct {
	Sender string
	Owners []string
}

type SenderTransport struct{ SenderKey, Transport string }

type SASLEntry struct{ Nexthop, Credentials string }

type TLSPolicy struct{ Nexthop, Policy string }

type DKIMKey struct{ Domain, Selector, KeyPath string }

// Builder assembles Data from the store and runtime config.
type Builder struct {
	Cfg    *config.Config
	Store  *db.Store
	Engine *delivery.Engine
	Secret []byte
	// MailUser is the system user owning Maildir storage ("wispbox" in
	// production; the current user in development).
	MailUser  string
	MailGroup string
	// DovecotV24 selects the Dovecot 2.4 templates (detected at runtime).
	DovecotV24 bool
}

// Build assembles the full render input. It fails loudly rather than
// producing a config that silently drops mail.
func (b *Builder) Build(ctx context.Context) (*Data, error) {
	primary := b.Store.GetSettingDefault(ctx, "primary_hostname", "")
	if primary == "" {
		primary = "localhost"
	}

	d := &Data{
		PrimaryHostname:      primary,
		PostfixMapDir:        filepath.Join(b.Cfg.GeneratedDir, "postfix"),
		GeneratedDovecotDir:  filepath.Join(b.Cfg.GeneratedDir, "dovecot"),
		GeneratedOpenDKIMDir: filepath.Join(b.Cfg.GeneratedDir, "opendkim"),
		MailDir:              b.Cfg.MailDir,
		DBPath:               b.Cfg.DBPath,
		MailUser:             b.MailUser,
		MailGroup:            b.MailGroup,
		MessageSizeLimit:     50 * 1024 * 1024,
		DefaultCertPath:      filepath.Join(b.Cfg.CertDir, "_default", "fullchain.pem"),
		DefaultKeyPath:       filepath.Join(b.Cfg.CertDir, "_default", "privkey.pem"),
		DovecotV24:           b.DovecotV24,
	}

	var err error
	if d.Domains, err = b.Store.ListDomains(ctx); err != nil {
		return nil, fmt.Errorf("load domains: %w", err)
	}
	if d.Mailboxes, err = b.Store.ListMailboxes(ctx, 0); err != nil {
		return nil, fmt.Errorf("load mailboxes: %w", err)
	}
	if d.Aliases, err = b.Store.ListAliases(ctx, 0); err != nil {
		return nil, fmt.Errorf("load aliases: %w", err)
	}

	if err := b.buildSenderLogins(d); err != nil {
		return nil, err
	}
	if err := b.buildTLS(ctx, d); err != nil {
		return nil, err
	}
	if err := b.buildRouting(ctx, d); err != nil {
		return nil, err
	}
	b.buildDKIM(d)
	return d, nil
}

// buildSenderLogins computes smtpd_sender_login_maps: which SASL logins may
// use which envelope sender. Every mailbox owns its own address; an enabled
// alias may be used by the local mailboxes it forwards to.
func (b *Builder) buildSenderLogins(d *Data) error {
	mailboxSet := map[string]bool{}
	for _, m := range d.Mailboxes {
		if m.Enabled {
			mailboxSet[m.Email] = true
		}
	}
	owners := map[string][]string{}
	for _, m := range d.Mailboxes {
		if m.Enabled {
			owners[m.Email] = append(owners[m.Email], m.Email)
		}
	}
	for _, a := range d.Aliases {
		if !a.Enabled || a.IsCatchAll {
			continue // catch-alls receive, they never grant send rights
		}
		if mailboxSet[a.Destination] {
			owners[a.Source] = append(owners[a.Source], a.Destination)
		}
	}
	keys := make([]string, 0, len(owners))
	for k := range owners {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		d.SenderLogins = append(d.SenderLogins, SenderLogin{Sender: k, Owners: owners[k]})
	}
	return nil
}

// buildTLS selects certificates that actually exist on disk for SNI.
func (b *Builder) buildTLS(ctx context.Context, d *Data) error {
	certs, err := b.Store.ListCertificates(ctx)
	if err != nil {
		return fmt.Errorf("load certificates: %w", err)
	}
	for _, c := range certs {
		if c.Status == db.CertActive && fileExists(c.CertPath) && fileExists(c.KeyPath) {
			d.SNI = append(d.SNI, SNIEntry{Hostname: c.Hostname, CertPath: c.CertPath, KeyPath: c.KeyPath})
		}
	}
	return nil
}

// buildRouting computes per-sender-domain transports from delivery policies.
func (b *Builder) buildRouting(ctx context.Context, d *Data) error {
	global, err := b.Engine.ResolveGlobal(ctx)
	if err != nil {
		return err
	}
	// nexthop -> credential string already emitted, so we can detect two
	// relays that share a host:port but carry different logins (Postfix keys
	// sasl_passwd by nexthop, so they cannot coexist — fail loudly rather
	// than silently authenticate one relay with the other's credentials).
	seenNexthop := map[string]string{}

	transportFor := func(r *delivery.Resolved) (string, error) {
		if r.Mode == db.ModeDirect {
			return "smtp:", nil
		}
		relay := r.Relay
		nexthop := fmt.Sprintf("[%s]:%d", relay.Host, relay.Port)
		transport := "smtp:" + nexthop
		if relay.TLSMode == db.TLSModeTLS {
			transport = "relaytls:" + nexthop
		}

		creds := ""
		if relay.Username != "" || relay.EncryptedPassword != "" {
			password, err := security.Decrypt(b.Secret, relay.EncryptedPassword)
			if err != nil {
				return "", fmt.Errorf("relay %q: cannot decrypt stored password (was the secret key replaced?): %w", relay.Name, err)
			}
			creds = relay.Username + ":" + password
		}

		if prev, ok := seenNexthop[nexthop]; ok {
			if prev != creds {
				return "", fmt.Errorf("two relays point at %s with different credentials; Postfix cannot distinguish them — give them distinct hosts or merge them", nexthop)
			}
		} else {
			seenNexthop[nexthop] = creds
			// A credential-less relay (rare, e.g. IP-authenticated) still
			// works, but emits no sasl_passwd line to avoid a ":" entry that
			// would make Postfix send empty AUTH.
			if creds != "" {
				d.SASLEntries = append(d.SASLEntries, SASLEntry{Nexthop: nexthop, Credentials: creds})
			}
			if relay.TLSMode == db.TLSModeStartTLS {
				d.TLSPolicies = append(d.TLSPolicies, TLSPolicy{Nexthop: nexthop, Policy: "encrypt"})
			}
		}
		return transport, nil
	}

	if global.Mode == db.ModeDirect {
		d.DefaultTransport = "smtp"
	} else {
		t, err := transportFor(global)
		if err != nil {
			return err
		}
		d.DefaultTransport = t
	}

	for _, dom := range d.Domains {
		resolved, err := b.Engine.ResolveForDomain(ctx, dom.ID)
		if err != nil {
			return fmt.Errorf("domain %s: %w", dom.Name, err)
		}
		if resolved.Source != "domain" {
			continue // inherits global; no map entry needed
		}
		t, err := transportFor(resolved)
		if err != nil {
			return fmt.Errorf("domain %s: %w", dom.Name, err)
		}
		d.SenderTransports = append(d.SenderTransports, SenderTransport{
			SenderKey: "@" + dom.Name,
			Transport: t,
		})
	}
	return nil
}

func (b *Builder) buildDKIM(d *Data) {
	for _, dom := range d.Domains {
		keyPath := filepath.Join(b.Cfg.DKIMDir, dom.Name, DKIMSelector+".private")
		if fileExists(keyPath) {
			d.DKIMKeys = append(d.DKIMKeys, DKIMKey{Domain: dom.Name, Selector: DKIMSelector, KeyPath: keyPath})
		}
	}
	d.DKIMEnabled = len(d.DKIMKeys) > 0
}

// Sanity check before any file is written: a config with no domains is valid
// (fresh install), but inconsistent internal state is not.
func (d *Data) Validate() error {
	if d.PrimaryHostname == "" {
		return fmt.Errorf("primary hostname is empty")
	}
	if d.DefaultTransport == "" {
		return fmt.Errorf("default transport is empty")
	}
	for _, m := range d.Mailboxes {
		if !strings.Contains(m.Email, "@") {
			return fmt.Errorf("mailbox %d has malformed address %q", m.ID, m.Email)
		}
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
