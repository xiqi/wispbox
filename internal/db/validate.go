package db

import (
	"fmt"
	"net"
	"net/mail"
	"regexp"
	"strings"
)

var (
	adminUsernameRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{1,30}[a-z0-9])?$`)
	domainRe        = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	localPartRe     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._+-]{0,62}[a-z0-9])?$|^[a-z0-9]$`)
)

// ValidateAdminUsername checks the local username used only for /admin sign-in.
func ValidateAdminUsername(username string) error {
	username = strings.TrimSpace(username)
	if username == "" {
		return fmt.Errorf("admin username is required")
	}
	if username != strings.ToLower(username) {
		return fmt.Errorf("admin username must be lowercase")
	}
	if !adminUsernameRe.MatchString(username) {
		return fmt.Errorf("admin username must be 3-32 lowercase letters, numbers, dots, dashes, or underscores")
	}
	return nil
}

// ValidateDomainName checks that name is a plausible bare mail domain
// (e.g. "example.com"), lowercase, no scheme, no trailing dot.
func ValidateDomainName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("domain name is required")
	}
	if name != strings.ToLower(name) {
		return fmt.Errorf("domain name must be lowercase")
	}
	if strings.Contains(name, "://") || strings.Contains(name, "/") {
		return fmt.Errorf("enter a bare domain like example.com, not a URL")
	}
	if strings.HasPrefix(name, "mail.") {
		return fmt.Errorf("enter the base domain (example.com); wispbox adds the mail. hostname for you")
	}
	if len(name) > 253 {
		return fmt.Errorf("domain name is too long")
	}
	if !domainRe.MatchString(name) {
		return fmt.Errorf("%q is not a valid domain name", name)
	}
	return nil
}

// ValidateHostname checks a fully qualified hostname such as mail.example.com.
func ValidateHostname(name string) error {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return fmt.Errorf("hostname is required")
	}
	if len(name) > 253 || !domainRe.MatchString(name) {
		return fmt.Errorf("%q is not a valid hostname", name)
	}
	return nil
}

// DefaultMailHostname returns the conventional mail hostname for a domain.
func DefaultMailHostname(domain string) string {
	return "mail." + strings.ToLower(strings.TrimSpace(domain))
}

// ValidateLocalPart checks the part before @ for a new mailbox or alias.
func ValidateLocalPart(lp string) error {
	lp = strings.TrimSpace(lp)
	if lp == "" {
		return fmt.Errorf("address local part is required")
	}
	if lp != strings.ToLower(lp) {
		return fmt.Errorf("address must be lowercase")
	}
	if len(lp) > 64 {
		return fmt.Errorf("address local part is too long")
	}
	if !localPartRe.MatchString(lp) {
		return fmt.Errorf("%q contains characters that are not allowed", lp)
	}
	if strings.Contains(lp, "..") {
		return fmt.Errorf("address cannot contain consecutive dots")
	}
	return nil
}

// ValidateIPv4 checks that v is a syntactically valid IPv4 address.
func ValidateIPv4(v string) error {
	ip := net.ParseIP(strings.TrimSpace(v))
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("%q is not a valid IPv4 address", v)
	}
	return nil
}

// ValidateIPv6 checks that v is a syntactically valid IPv6 address.
func ValidateIPv6(v string) error {
	ip := net.ParseIP(strings.TrimSpace(v))
	if ip == nil || ip.To4() != nil {
		return fmt.Errorf("%q is not a valid IPv6 address", v)
	}
	return nil
}

// ValidateEmail checks a full RFC 5322 address.
func ValidateEmail(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fmt.Errorf("email address is required")
	}
	parsed, err := mail.ParseAddress(addr)
	if err != nil || parsed.Address != addr {
		return fmt.Errorf("%q is not a valid email address", addr)
	}
	return nil
}
