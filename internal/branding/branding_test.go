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
