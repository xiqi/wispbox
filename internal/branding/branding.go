// Package branding keeps the user-facing product name and logo in one small
// settings-backed shape.
package branding

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultName = "wispbox"

	SettingName = "brand_name"
	SettingLogo = "brand_logo"

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
