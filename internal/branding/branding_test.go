package branding

import (
	"context"
	"strings"
	"testing"
)

type fakeStore map[string]string

func (f fakeStore) GetSettingDefault(_ context.Context, key, def string) string {
	if v, ok := f[key]; ok {
		return v
	}
	return def
}

func TestCurrentDefaultsAndOverrides(t *testing.T) {
	ctx := context.Background()
	if got := Current(ctx, fakeStore{}); got.Name != DefaultName || got.Logo != "" {
		t.Fatalf("Current(empty) = %+v, want default name and no logo", got)
	}

	got := Current(ctx, fakeStore{SettingName: "  Acme Mail  ", SettingLogo: " data:image/png;base64,x "})
	if got.Name != "Acme Mail" {
		t.Errorf("Name = %q, want Acme Mail", got.Name)
	}
	if got.Logo != "data:image/png;base64,x" {
		t.Errorf("Logo = %q, want trimmed data URL", got.Logo)
	}
}

func TestCurrentForHostUsesDomainOverride(t *testing.T) {
	ctx := context.Background()
	store := fakeStore{
		SettingName:                          "Global Mail",
		SettingLogo:                          "data:image/png;base64,global",
		DomainSettingName("example.com"):     "Example Mail",
		DomainSettingLogo("startup.example"): "data:image/png;base64,startup",
	}

	got := CurrentForHost(ctx, store, "mail.example.com:443")
	if got.Name != "Example Mail" {
		t.Errorf("Name = %q, want Example Mail", got.Name)
	}
	if got.Logo != "data:image/png;base64,global" {
		t.Errorf("Logo = %q, want inherited global logo", got.Logo)
	}

	got = CurrentForHost(ctx, store, "startup.example")
	if got.Name != "Global Mail" {
		t.Errorf("Name = %q, want inherited global name", got.Name)
	}
	if got.Logo != "data:image/png;base64,startup" {
		t.Errorf("Logo = %q, want startup logo", got.Logo)
	}
}

func TestParseDomainSettingKey(t *testing.T) {
	domain, setting, ok := ParseDomainSettingKey("brand_domain:Example.COM.:brand_name")
	if !ok || domain != "example.com" || setting != SettingName {
		t.Fatalf("ParseDomainSettingKey = %q, %q, %v", domain, setting, ok)
	}
	if _, _, ok := ParseDomainSettingKey("brand_domain:example.com:acme_email"); ok {
		t.Fatal("non-brand domain setting accepted")
	}
}

func TestValidateName(t *testing.T) {
	if err := ValidateName(""); err != nil {
		t.Fatalf("empty name should reset to default: %v", err)
	}
	if err := ValidateName(strings.Repeat("x", MaxNameRunes+1)); err == nil {
		t.Fatal("overlong name accepted")
	}
	if err := ValidateName("bad\nname"); err == nil {
		t.Fatal("control character accepted")
	}
	if err := ValidateName("Acme Mail"); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
}

func TestLogoDataURL(t *testing.T) {
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'H', 'D', 'R'}
	got, err := LogoDataURL(png)
	if err != nil {
		t.Fatalf("LogoDataURL(png): %v", err)
	}
	if !strings.HasPrefix(got, "data:image/png;base64,") {
		t.Fatalf("LogoDataURL(png) = %q, want png data URL", got)
	}

	if _, err := LogoDataURL([]byte("<svg></svg>")); err == nil {
		t.Fatal("svg logo accepted")
	}
	if _, err := LogoDataURL(make([]byte, MaxLogoBytes+1)); err == nil {
		t.Fatal("oversized logo accepted")
	}
}
