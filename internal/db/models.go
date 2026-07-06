package db

import "time"

type Admin struct {
	ID                  int64  `json:"id"`
	Username            string `json:"username"`
	PasswordHash        string `json:"-"`
	WebAuthnHandle      string `json:"-"`
	TwoFactorEnabled    bool   `json:"two_factor_enabled"`
	EncryptedTOTPSecret string `json:"-"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

type DomainStatus string

const (
	DomainPending DomainStatus = "pending"
	DomainActive  DomainStatus = "active"
	DomainError   DomainStatus = "error"
)

type Domain struct {
	ID           int64        `json:"id"`
	Name         string       `json:"name"`
	MailHostname string       `json:"mail_hostname"`
	Status       DomainStatus `json:"status"`
	CreatedAt    string       `json:"created_at"`
	UpdatedAt    string       `json:"updated_at"`
}

type Mailbox struct {
	ID                       int64  `json:"id"`
	DomainID                 int64  `json:"domain_id"`
	LocalPart                string `json:"local_part"`
	Email                    string `json:"email"`
	PasswordHash             string `json:"-"`
	WebAuthnHandle           string `json:"-"`
	TwoFactorEnabled         bool   `json:"two_factor_enabled"`
	EncryptedTOTPSecret      string `json:"-"`
	EncryptedPasskeyPassword string `json:"-"`
	QuotaMB                  int64  `json:"quota_mb"`
	Enabled                  bool   `json:"enabled"`
	CreatedAt                string `json:"created_at"`
	UpdatedAt                string `json:"updated_at"`
}

type Alias struct {
	ID          int64  `json:"id"`
	DomainID    int64  `json:"domain_id"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	IsCatchAll  bool   `json:"is_catch_all"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type TLSMode string

const (
	TLSModeStartTLS TLSMode = "starttls"
	TLSModeTLS      TLSMode = "tls"
	TLSModeNone     TLSMode = "none"
)

type OutboundRelay struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	Provider          string  `json:"provider"`
	Host              string  `json:"host"`
	Port              int     `json:"port"`
	Username          string  `json:"username"`
	EncryptedPassword string  `json:"-"`
	TLSMode           TLSMode `json:"tls_mode"`
	Enabled           bool    `json:"enabled"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

type PolicyScope string

const (
	ScopeGlobal  PolicyScope = "global"
	ScopeDomain  PolicyScope = "domain"
	ScopeMailbox PolicyScope = "mailbox"
)

type DeliveryMode string

const (
	ModeDirect  DeliveryMode = "direct"
	ModeRelay   DeliveryMode = "relay"
	ModeInherit DeliveryMode = "inherit"
)

type OutboundPolicy struct {
	ID        int64        `json:"id"`
	ScopeType PolicyScope  `json:"scope_type"`
	ScopeID   int64        `json:"scope_id"`
	Mode      DeliveryMode `json:"mode"`
	RelayID   *int64       `json:"relay_id"`
	Enabled   bool         `json:"enabled"`
	CreatedAt string       `json:"created_at"`
	UpdatedAt string       `json:"updated_at"`
}

type CertStatus string

const (
	CertPending CertStatus = "pending"
	CertDNSWait CertStatus = "dns_wait"
	CertIssuing CertStatus = "issuing"
	CertActive  CertStatus = "active"
	CertError   CertStatus = "error"
)

type Certificate struct {
	ID            int64      `json:"id"`
	DomainID      int64      `json:"domain_id"`
	Hostname      string     `json:"hostname"`
	Status        CertStatus `json:"status"`
	ChallengeType string     `json:"challenge_type"`
	CertPath      string     `json:"cert_path"`
	KeyPath       string     `json:"key_path"`
	NotBefore     string     `json:"not_before"`
	NotAfter      string     `json:"not_after"`
	LastRenewedAt string     `json:"last_renewed_at"`
	RenewAfter    string     `json:"renew_after"`
	LastError     string     `json:"last_error"`
	CreatedAt     string     `json:"created_at"`
	UpdatedAt     string     `json:"updated_at"`
}

// NotAfterTime returns the parsed expiry, if set.
func (c *Certificate) NotAfterTime() (time.Time, bool) { return ParseTime(c.NotAfter) }

// RenewAfterTime returns the parsed renewal threshold, if set.
func (c *Certificate) RenewAfterTime() (time.Time, bool) { return ParseTime(c.RenewAfter) }

type UserType string

const (
	UserAdmin   UserType = "admin"
	UserMailbox UserType = "mailbox"
)

type Session struct {
	ID        string
	UserType  UserType
	UserID    int64
	ExpiresAt string
	CreatedAt string
}

type Passkey struct {
	ID                  int64    `json:"id"`
	UserType            UserType `json:"-"`
	UserID              int64    `json:"-"`
	RPID                string   `json:"rp_id"`
	CredentialID        string   `json:"-"`
	Name                string   `json:"name"`
	EncryptedCredential string   `json:"-"`
	CreatedAt           string   `json:"created_at"`
	UpdatedAt           string   `json:"updated_at"`
	LastUsedAt          string   `json:"last_used_at"`
}

type AuthChallenge struct {
	ID          string
	UserType    UserType
	UserID      int64
	Kind        string
	SessionData string
	CreatedAt   string
	ExpiresAt   string
}

type AuditLog struct {
	ID         int64  `json:"id"`
	ActorType  string `json:"actor_type"`
	ActorID    int64  `json:"actor_id"`
	Action     string `json:"action"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	IP         string `json:"ip"`
	CreatedAt  string `json:"created_at"`
}

type ServiceEvent struct {
	ID        int64  `json:"id"`
	Service   string `json:"service"`
	EventType string `json:"event_type"`
	Status    string `json:"status"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}
