// Package delivery implements the outbound delivery policy engine:
// which domains send direct and which send through a relay.
//
// Policy resolution for v0 is two levels: a per-domain override, else the
// global default. (Mailbox-level scope exists in the data model for later.)
// wispbox never silently falls back from relay to direct — if a relay is
// selected and broken, mail stays queued and the error surfaces in Admin.
package delivery

import (
	"context"
	"fmt"
	"strings"

	"github.com/xiqi/wispbox/internal/db"
)

// Provider presets for well-known relay services. Host/port are editable
// defaults, not constraints.
type ProviderPreset struct {
	Provider     string     `json:"provider"`
	Label        string     `json:"label"`
	Host         string     `json:"host"`
	Port         int        `json:"port"`
	TLSMode      db.TLSMode `json:"tls_mode"`
	UsernameHint string     `json:"username_hint"`
	SPFInclude   string     `json:"spf_include"`
	Note         string     `json:"note"`
}

// Presets returns the built-in relay provider presets.
func Presets() []ProviderPreset {
	return []ProviderPreset{
		{Provider: "ses", Label: "Amazon SES", Host: "email-smtp.us-east-1.amazonaws.com", Port: 587, TLSMode: db.TLSModeStartTLS,
			UsernameHint: "SMTP username from SES console", SPFInclude: "include:amazonses.com",
			Note: "Adjust the host to your SES region, e.g. email-smtp.eu-west-1.amazonaws.com."},
		{Provider: "postmark", Label: "Postmark", Host: "smtp.postmarkapp.com", Port: 587, TLSMode: db.TLSModeStartTLS,
			UsernameHint: "Server API token", SPFInclude: "include:spf.mtasv.net",
			Note: "Use your Server API token as both username and password."},
		{Provider: "mailgun", Label: "Mailgun", Host: "smtp.mailgun.org", Port: 587, TLSMode: db.TLSModeStartTLS,
			UsernameHint: "postmaster@yourdomain", SPFInclude: "include:mailgun.org",
			Note: "Create SMTP credentials in the Mailgun dashboard for your sending domain."},
		{Provider: "smtp2go", Label: "SMTP2GO", Host: "mail.smtp2go.com", Port: 587, TLSMode: db.TLSModeStartTLS,
			UsernameHint: "SMTP2GO username", SPFInclude: "include:spf.smtp2go.com",
			Note: "Port 2525 also works if 587 is blocked."},
		{Provider: "resend", Label: "Resend", Host: "smtp.resend.com", Port: 465, TLSMode: db.TLSModeTLS,
			UsernameHint: "resend", SPFInclude: "include:amazonses.com",
			Note: "Username is the literal string 'resend'; password is your API key. Confirm the SPF include in your Resend dashboard."},
		{Provider: "custom", Label: "Custom SMTP", Host: "", Port: 587, TLSMode: db.TLSModeStartTLS,
			UsernameHint: "", SPFInclude: "", Note: "Any SMTP submission endpoint with STARTTLS or implicit TLS."},
	}
}

// PresetFor returns the preset for a provider id, if known.
func PresetFor(provider string) (ProviderPreset, bool) {
	for _, p := range Presets() {
		if p.Provider == provider {
			return p, true
		}
	}
	return ProviderPreset{}, false
}

// hasControlChars reports whether s contains any control character (CR, LF,
// tab, NUL, …). Such characters would let a field break out of a Postfix map
// line or main.cf directive when the config is generated.
func hasControlChars(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0
}

// ValidateRelay checks a relay definition before it is stored.
func ValidateRelay(r db.OutboundRelay) error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("relay name is required")
	}
	if hasControlChars(r.Name) {
		return fmt.Errorf("relay name contains control characters")
	}
	if _, ok := PresetFor(r.Provider); !ok {
		return fmt.Errorf("unknown relay provider %q", r.Provider)
	}
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return fmt.Errorf("relay host is required")
	}
	if strings.ContainsAny(host, " /:") || hasControlChars(host) {
		return fmt.Errorf("relay host must be a bare hostname without port or scheme")
	}
	if hasControlChars(r.Username) {
		return fmt.Errorf("relay username contains control characters")
	}
	if r.Port < 1 || r.Port > 65535 {
		return fmt.Errorf("relay port must be between 1 and 65535")
	}
	switch r.TLSMode {
	case db.TLSModeStartTLS, db.TLSModeTLS:
	case db.TLSModeNone:
		return fmt.Errorf("unencrypted relays are not supported; use STARTTLS or TLS")
	default:
		return fmt.Errorf("invalid TLS mode %q", r.TLSMode)
	}
	return nil
}

// ValidateRelayPassword rejects control characters in a relay password. The
// decrypted password is written verbatim into the Postfix sasl_passwd map, so
// a CR/LF/NUL could break out of the map line.
func ValidateRelayPassword(password string) error {
	if hasControlChars(password) {
		return fmt.Errorf("relay password contains control characters")
	}
	return nil
}

// Resolved is the effective delivery decision for a scope.
type Resolved struct {
	Mode   db.DeliveryMode   `json:"mode"` // direct or relay (never inherit)
	Relay  *db.OutboundRelay `json:"relay,omitempty"`
	Source string            `json:"source"` // "domain" or "global"
}

// Engine resolves effective policies from the store.
type Engine struct {
	store *db.Store
}

func NewEngine(store *db.Store) *Engine { return &Engine{store: store} }

// ResolveForDomain returns the effective policy for a domain: the domain
// override when present and not "inherit", otherwise the global default.
func (e *Engine) ResolveForDomain(ctx context.Context, domainID int64) (*Resolved, error) {
	if p, err := e.store.GetPolicyForScope(ctx, db.ScopeDomain, domainID); err == nil && p.Enabled && p.Mode != db.ModeInherit {
		return e.materialize(ctx, p, "domain")
	}
	return e.ResolveGlobal(ctx)
}

// ResolveGlobal returns the global default policy.
func (e *Engine) ResolveGlobal(ctx context.Context) (*Resolved, error) {
	p, err := e.store.GetPolicyForScope(ctx, db.ScopeGlobal, 0)
	if err != nil {
		return nil, fmt.Errorf("global delivery policy missing: %w", err)
	}
	if p.Mode == db.ModeInherit {
		// The global scope has nothing to inherit from; treat as direct.
		return &Resolved{Mode: db.ModeDirect, Source: "global"}, nil
	}
	return e.materialize(ctx, p, "global")
}

func (e *Engine) materialize(ctx context.Context, p *db.OutboundPolicy, source string) (*Resolved, error) {
	res := &Resolved{Mode: p.Mode, Source: source}
	if p.Mode == db.ModeRelay {
		if p.RelayID == nil {
			return nil, fmt.Errorf("%s policy is set to relay but no relay is selected", source)
		}
		relay, err := e.store.GetRelay(ctx, *p.RelayID)
		if err != nil {
			return nil, fmt.Errorf("%s policy references a relay that no longer exists", source)
		}
		if !relay.Enabled {
			return nil, fmt.Errorf("relay %q is disabled; enable it or change the delivery policy", relay.Name)
		}
		res.Relay = relay
	}
	return res, nil
}

// SetPolicy validates and stores a policy for a scope.
func (e *Engine) SetPolicy(ctx context.Context, scope db.PolicyScope, scopeID int64, mode db.DeliveryMode, relayID *int64) (*db.OutboundPolicy, error) {
	switch mode {
	case db.ModeDirect, db.ModeRelay, db.ModeInherit:
	default:
		return nil, fmt.Errorf("invalid delivery mode %q", mode)
	}
	if scope == db.ScopeGlobal && mode == db.ModeInherit {
		return nil, fmt.Errorf("the global policy cannot be 'inherit'; choose direct or relay")
	}
	if mode == db.ModeRelay {
		if relayID == nil {
			return nil, fmt.Errorf("relay mode requires selecting a relay")
		}
		relay, err := e.store.GetRelay(ctx, *relayID)
		if err != nil {
			return nil, fmt.Errorf("selected relay not found")
		}
		if !relay.Enabled {
			return nil, fmt.Errorf("relay %q is disabled", relay.Name)
		}
	} else {
		relayID = nil
	}
	return e.store.UpsertPolicy(ctx, scope, scopeID, mode, relayID)
}
