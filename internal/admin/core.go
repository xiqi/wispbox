// Package admin implements the Admin REST API at /api/admin/* and the
// business operations behind it. The Core type is shared with the setup
// wizard so both surfaces behave identically.
package admin

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/xiqi/wispbox/internal/auth"
	"github.com/xiqi/wispbox/internal/certs"
	"github.com/xiqi/wispbox/internal/config"
	"github.com/xiqi/wispbox/internal/configgen"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/delivery"
	"github.com/xiqi/wispbox/internal/dnscheck"
	"github.com/xiqi/wispbox/internal/mailstore"
	"github.com/xiqi/wispbox/internal/netcheck"
	"github.com/xiqi/wispbox/internal/security"
)

// Core executes admin-level operations against the store and regenerates
// mail server configuration after every mutation.
type Core struct {
	Cfg       *config.Config
	Store     *db.Store
	Engine    *delivery.Engine
	Generator *configgen.Generator
	Certs     *certs.Manager
	Checker   *dnscheck.Checker
	Secret    []byte
	Log       *slog.Logger

	OutboundSMTP25Open func(ctx context.Context) bool
}

// regen regenerates Postfix/Dovecot/OpenDKIM config after a mutation.
// A regeneration failure never rolls back the database change: the previous
// generated config stays live, the error is recorded, and the returned
// warning surfaces in the Admin UI.
func (c *Core) regen(ctx context.Context) (warning string) {
	if err := c.Generator.Apply(ctx); err != nil {
		c.Log.Error("config regeneration failed", "error", err)
		return "Saved, but mail server configuration could not be regenerated: " + err.Error()
	}
	return ""
}

// ---- domains ----

// CreateDomain provisions a domain: DB row, DKIM key, certificate tracking,
// and regenerated mail config.
func (c *Core) CreateDomain(ctx context.Context, name, mailHostname string) (*db.Domain, string, error) {
	dom, err := c.Store.CreateDomain(ctx, name, mailHostname)
	if err != nil {
		return nil, "", err
	}
	if _, err := configgen.EnsureDKIMKey(c.Cfg.DKIMDir, dom.Name); err != nil {
		c.Log.Warn("DKIM key generation failed", "domain", dom.Name, "error", err)
	}
	if _, err := c.Certs.EnsureTracked(ctx, dom.ID, dom.MailHostname); err != nil {
		c.Log.Warn("certificate tracking failed", "hostname", dom.MailHostname, "error", err)
	}
	return dom, c.regen(ctx), nil
}

// DeleteDomain removes a domain and returns the deleted row so callers can
// name it in the audit log.
func (c *Core) DeleteDomain(ctx context.Context, id int64) (*db.Domain, string, error) {
	dom, err := c.Store.GetDomain(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if err := c.Store.DeleteDomain(ctx, id); err != nil {
		return nil, "", err
	}
	return dom, c.regen(ctx), nil
}

// ---- mailboxes ----

func (c *Core) CreateMailbox(ctx context.Context, domainID int64, localPart, password string, quotaMB int64) (*db.Mailbox, string, error) {
	hash, err := auth.HashMailboxPassword(password)
	if err != nil {
		return nil, "", err
	}
	mb, err := c.Store.CreateMailbox(ctx, domainID, localPart, hash, quotaMB)
	if err != nil {
		return nil, "", err
	}
	if err := mailstore.EnsureMaildir(c.Cfg.MailDir, mb.Email); err != nil {
		c.Log.Warn("maildir provisioning failed", "email", mb.Email, "error", err)
	}
	return mb, c.regen(ctx), nil
}

// UpdateMailbox applies optional quota/enabled changes (nil fields keep the
// current value), regenerates config, and returns the updated mailbox.
func (c *Core) UpdateMailbox(ctx context.Context, id int64, quotaMB *int64, enabled *bool) (*db.Mailbox, string, error) {
	mb, err := c.Store.GetMailbox(ctx, id)
	if err != nil {
		return nil, "", err
	}
	quota := mb.QuotaMB
	if quotaMB != nil {
		quota = *quotaMB
		if quota < 0 {
			quota = 0
		}
	}
	on := mb.Enabled
	if enabled != nil {
		on = *enabled
	}
	if err := c.Store.UpdateMailbox(ctx, id, quota, on); err != nil {
		return nil, "", err
	}
	updated, err := c.Store.GetMailbox(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return updated, c.regen(ctx), nil
}

// ResetMailboxPassword sets a new password and returns the mailbox (so callers
// can name it in the audit log). Passwords are not part of generated config, so
// no regeneration is needed.
func (c *Core) ResetMailboxPassword(ctx context.Context, id int64, password string) (*db.Mailbox, error) {
	if password == "" {
		return nil, fmt.Errorf("new password is required")
	}
	mb, err := c.Store.GetMailbox(ctx, id)
	if err != nil {
		return nil, err
	}
	hash, err := auth.HashMailboxPassword(password)
	if err != nil {
		return nil, err
	}
	encryptedPasskeyPassword, err := security.Encrypt(c.Secret, password)
	if err != nil {
		return nil, err
	}
	if err := c.Store.UpdateMailboxPasswordAndPasskeySecret(ctx, id, hash, encryptedPasskeyPassword); err != nil {
		return nil, err
	}
	return mb, nil
}

// DeleteMailbox removes a mailbox and returns the deleted row for audit
// logging.
func (c *Core) DeleteMailbox(ctx context.Context, id int64) (*db.Mailbox, string, error) {
	mb, err := c.Store.GetMailbox(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if err := c.Store.DeleteMailbox(ctx, id); err != nil {
		return nil, "", err
	}
	return mb, c.regen(ctx), nil
}

// ---- aliases ----

// CreateAlias creates an alias and regenerates config.
func (c *Core) CreateAlias(ctx context.Context, domainID int64, source, destination string, isCatchAll bool) (*db.Alias, string, error) {
	alias, err := c.Store.CreateAlias(ctx, domainID, source, destination, isCatchAll)
	if err != nil {
		return nil, "", err
	}
	return alias, c.regen(ctx), nil
}

// UpdateAliasEnabled toggles an alias, regenerates config, and returns the
// updated row.
func (c *Core) UpdateAliasEnabled(ctx context.Context, id int64, enabled bool) (*db.Alias, string, error) {
	if err := c.Store.UpdateAliasEnabled(ctx, id, enabled); err != nil {
		return nil, "", err
	}
	alias, err := c.Store.GetAlias(ctx, id)
	if err != nil {
		return nil, "", err
	}
	return alias, c.regen(ctx), nil
}

// DeleteAlias removes an alias and returns the deleted row for audit logging.
func (c *Core) DeleteAlias(ctx context.Context, id int64) (*db.Alias, string, error) {
	alias, err := c.Store.GetAlias(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if err := c.Store.DeleteAlias(ctx, id); err != nil {
		return nil, "", err
	}
	return alias, c.regen(ctx), nil
}

// ---- relays ----

// SaveRelay validates and stores a relay; password arrives in plaintext and
// is encrypted with the instance secret before it touches the database.
func (c *Core) SaveRelay(ctx context.Context, id int64, name, provider, host string, port int, username, password string, tlsMode db.TLSMode, enabled bool) (*db.OutboundRelay, string, error) {
	relay := db.OutboundRelay{
		ID: id, Name: strings.TrimSpace(name), Provider: provider,
		Host: strings.TrimSpace(host), Port: port,
		Username: username, TLSMode: tlsMode, Enabled: enabled,
	}
	if err := delivery.ValidateRelay(relay); err != nil {
		return nil, "", err
	}
	if err := delivery.ValidateRelayPassword(password); err != nil {
		return nil, "", err
	}
	if password != "" {
		enc, err := security.Encrypt(c.Secret, password)
		if err != nil {
			return nil, "", err
		}
		relay.EncryptedPassword = enc
	}

	var saved *db.OutboundRelay
	var err error
	if id == 0 {
		if relay.EncryptedPassword == "" && relay.Username != "" {
			return nil, "", fmt.Errorf("relay password is required")
		}
		saved, err = c.Store.CreateRelay(ctx, relay)
	} else {
		existing, gerr := c.Store.GetRelay(ctx, id)
		if gerr != nil {
			return nil, "", gerr
		}
		if relay.EncryptedPassword == "" {
			relay.EncryptedPassword = existing.EncryptedPassword // unchanged
		}
		err = c.Store.UpdateRelay(ctx, relay)
		if err == nil {
			saved, err = c.Store.GetRelay(ctx, id)
		}
	}
	if err != nil {
		return nil, "", err
	}
	return saved, c.regen(ctx), nil
}

// DeleteRelay removes a relay and returns the deleted row for audit logging.
func (c *Core) DeleteRelay(ctx context.Context, id int64) (*db.OutboundRelay, string, error) {
	relay, err := c.Store.GetRelay(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if err := c.Store.DeleteRelay(ctx, id); err != nil {
		return nil, "", err
	}
	return relay, c.regen(ctx), nil
}

// ---- delivery policies ----

// UpsertPolicy stores a delivery policy for a scope and regenerates config.
func (c *Core) UpsertPolicy(ctx context.Context, scope db.PolicyScope, scopeID int64, mode db.DeliveryMode, relayID *int64) (*db.OutboundPolicy, string, error) {
	if mode == db.ModeDirect && !c.directSendingAvailable(ctx) {
		return nil, "", fmt.Errorf("outbound port 25 is not available on this server; choose SMTP relay instead")
	}
	policy, err := c.Engine.SetPolicy(ctx, scope, scopeID, mode, relayID)
	if err != nil {
		return nil, "", err
	}
	return policy, c.regen(ctx), nil
}

func (c *Core) directSendingAvailable(ctx context.Context) bool {
	if c.Cfg.IsDev() {
		return true
	}
	status := c.OutboundSMTP25Status(ctx)
	return status != nil && *status
}

func (c *Core) OutboundSMTP25Status(ctx context.Context) *bool {
	if c.Cfg.IsDev() {
		return nil
	}
	var ok bool
	if c.OutboundSMTP25Open != nil {
		ok = c.OutboundSMTP25Open(ctx)
	} else {
		ok = netcheck.OutboundSMTP25Open(ctx)
	}
	return &ok
}

// DeletePolicy removes a delivery policy and regenerates config.
func (c *Core) DeletePolicy(ctx context.Context, id int64) (string, error) {
	if err := c.Store.DeletePolicy(ctx, id); err != nil {
		return "", err
	}
	return c.regen(ctx), nil
}

// ---- DNS ----

// DNSRecords computes the required records for a domain, optionally checking
// them against live DNS.
func (c *Core) DNSRecords(ctx context.Context, domainID int64, check bool) (*db.Domain, []dnscheck.Record, error) {
	dom, err := c.Store.GetDomain(ctx, domainID)
	if err != nil {
		return nil, nil, err
	}
	in := dnscheck.Inputs{
		Domain:       dom.Name,
		MailHostname: dom.MailHostname,
		ServerIPv4:   c.Store.GetSettingDefault(ctx, "server_ipv4", ""),
		ServerIPv6:   c.Store.GetSettingDefault(ctx, "server_ipv6", ""),
		DKIMSelector: configgen.DKIMSelector,
	}
	if txt, err := configgen.DKIMTXTValue(c.Cfg.DKIMDir, dom.Name); err == nil {
		in.DKIMTXTValue = txt
	}
	if resolved, err := c.Engine.ResolveForDomain(ctx, dom.ID); err == nil && resolved.Relay != nil {
		if preset, ok := delivery.PresetFor(resolved.Relay.Provider); ok {
			in.SPFInclude = preset.SPFInclude
		}
	}
	records := dnscheck.RequiredRecords(in)
	if check {
		records = c.Checker.Check(ctx, records)
		c.updateDomainHealth(ctx, dom, records)
	}
	return dom, records, nil
}

// updateDomainHealth flips domain status based on the essential records
// (A + MX). SPF/DKIM/DMARC affect deliverability, not basic operation.
func (c *Core) updateDomainHealth(ctx context.Context, dom *db.Domain, records []dnscheck.Record) {
	status := db.DomainActive
	for _, r := range records {
		if (r.Purpose == "a" || r.Purpose == "mx") && r.Status != dnscheck.StatusOK {
			status = db.DomainPending
			break
		}
	}
	if dom.Status != status {
		_ = c.Store.UpdateDomainStatus(ctx, dom.ID, status)
		dom.Status = status
	}
}

// ServerIPs feeds the certificate manager's ACME preflight.
func (c *Core) ServerIPs(ctx context.Context) []string {
	var out []string
	if v4 := c.Store.GetSettingDefault(ctx, "server_ipv4", ""); v4 != "" {
		out = append(out, v4)
	}
	if v6 := c.Store.GetSettingDefault(ctx, "server_ipv6", ""); v6 != "" {
		out = append(out, v6)
	}
	return out
}
