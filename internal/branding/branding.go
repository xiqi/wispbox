// Package branding keeps the user-facing product name and logo in one small
// settings-backed shape.
package branding

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultName = "wispbox"

	SettingName = "brand_name"
	SettingLogo = "brand_logo"

	DomainSettingPrefix = "brand_domain:"

	MaxNameRunes = 40
	MaxLogoBytes = 256 << 10
)

// Store is the settings read surface used by the public brand endpoint.
type Store interface {
	GetSettingDefault(context.Context, string, string) string
}

// Brand is sent to the web UI.
type Brand struct {
	Name string `json:"name"`
	Logo string `json:"logo,omitempty"`
}

// Current returns the effective brand, falling back to the built-in defaults.
func Current(ctx context.Context, store Store) Brand {
	name := strings.TrimSpace(store.GetSettingDefault(ctx, SettingName, ""))
	if name == "" {
		name = DefaultName
	}
	return Brand{
		Name: name,
		Logo: strings.TrimSpace(store.GetSettingDefault(ctx, SettingLogo, "")),
	}
}

// CurrentForHost returns the effective brand for an HTTP host. Domain-specific
// settings override the global brand and inherit blank fields from it.
func CurrentForHost(ctx context.Context, store Store, host string) Brand {
	global := Current(ctx, store)
	for _, domain := range HostDomains(host) {
		name := strings.TrimSpace(store.GetSettingDefault(ctx, DomainSettingName(domain), ""))
		logo := strings.TrimSpace(store.GetSettingDefault(ctx, DomainSettingLogo(domain), ""))
		if name == "" && logo == "" {
			continue
		}
		if name == "" {
			name = global.Name
		}
		if logo == "" {
			logo = global.Logo
		}
		return Brand{Name: name, Logo: logo}
	}
	return global
}

// DomainSettingName returns the settings key for a domain-specific display name.
func DomainSettingName(domain string) string {
	return domainSettingKey(domain, SettingName)
}

// DomainSettingLogo returns the settings key for a domain-specific logo.
func DomainSettingLogo(domain string) string {
	return domainSettingKey(domain, SettingLogo)
}

func domainSettingKey(domain, setting string) string {
	return DomainSettingPrefix + NormalizeDomain(domain) + ":" + setting
}

// ParseDomainSettingKey extracts a domain-scoped brand setting key.
func ParseDomainSettingKey(key string) (domain, setting string, ok bool) {
	if !strings.HasPrefix(key, DomainSettingPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, DomainSettingPrefix)
	i := strings.LastIndex(rest, ":")
	if i <= 0 || i == len(rest)-1 {
		return "", "", false
	}
	setting = rest[i+1:]
	if setting != SettingName && setting != SettingLogo {
		return "", "", false
	}
	domain = NormalizeDomain(rest[:i])
	if domain == "" {
		return "", "", false
	}
	return domain, setting, true
}

// NormalizeDomain canonicalizes a setting-scope domain.
func NormalizeDomain(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if h, _, err := net.SplitHostPort(value); err == nil {
		value = h
	}
	value = strings.Trim(value, "[]")
	return strings.TrimSuffix(value, ".")
}

// HostDomains returns the exact host plus parent domains, longest first.
func HostDomains(host string) []string {
	host = NormalizeDomain(host)
	if host == "" {
		return nil
	}
	parts := strings.Split(host, ".")
	out := make([]string, 0, len(parts))
	for i := 0; i < len(parts); i++ {
		candidate := strings.Join(parts[i:], ".")
		if candidate == "" {
			continue
		}
		out = append(out, candidate)
	}
	return out
}

// ValidateName enforces a short display name suitable for headers and tabs.
func ValidateName(value string) error {
	if value == "" {
		return nil
	}
	if utf8.RuneCountInString(value) > MaxNameRunes {
		return fmt.Errorf("system name must be %d characters or fewer", MaxNameRunes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("system name cannot contain control characters")
		}
	}
	return nil
}

// LogoDataURL validates a logo upload and returns a self-contained data URL.
func LogoDataURL(data []byte) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("logo file is empty")
	}
	if len(data) > MaxLogoBytes {
		return "", fmt.Errorf("logo must be 256 KB or smaller")
	}
	contentType := logoContentType(data)
	if contentType == "" {
		return "", fmt.Errorf("logo must be a PNG, JPEG, or WebP image")
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func logoContentType(data []byte) string {
	switch http.DetectContentType(data) {
	case "image/png":
		return "image/png"
	case "image/jpeg":
		return "image/jpeg"
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	return ""
}
