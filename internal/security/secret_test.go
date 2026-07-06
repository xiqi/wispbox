package security

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateSecretRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "secret")

	key, err := LoadOrCreateSecret(path)
	if err != nil {
		t.Fatalf("LoadOrCreateSecret (create): %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("key length = %d, want 32", len(key))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat secret file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("secret file mode = %o, want 600", perm)
	}

	again, err := LoadOrCreateSecret(path)
	if err != nil {
		t.Fatalf("LoadOrCreateSecret (load): %v", err)
	}
	if !bytes.Equal(key, again) {
		t.Error("second load returned a different key")
	}
}

func TestLoadOrCreateSecretCorrupt(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{"not base64", "!!! definitely not base64 !!!"},
		{"wrong length", base64.StdEncoding.EncodeToString([]byte("short"))},
		{"empty file", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "secret")
			if err := os.WriteFile(path, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("write corrupt file: %v", err)
			}
			if _, err := LoadOrCreateSecret(path); err == nil {
				t.Fatal("LoadOrCreateSecret = nil error, want corrupt-file error")
			} else if !strings.Contains(err.Error(), "corrupt") {
				t.Fatalf("error = %q, want mention of corrupt file", err)
			}
		})
	}
}

func TestEncryptDecryptRoundtrip(t *testing.T) {
	key, err := LoadOrCreateSecret(filepath.Join(t.TempDir(), "secret"))
	if err != nil {
		t.Fatalf("LoadOrCreateSecret: %v", err)
	}

	for _, plaintext := range []string{"", "hunter2", "relay password with spaces & symbols: ☃"} {
		sealed, err := Encrypt(key, plaintext)
		if err != nil {
			t.Fatalf("Encrypt(%q): %v", plaintext, err)
		}
		if plaintext != "" && strings.Contains(sealed, plaintext) {
			t.Errorf("ciphertext contains plaintext %q", plaintext)
		}
		got, err := Decrypt(key, sealed)
		if err != nil {
			t.Fatalf("Decrypt(%q): %v", plaintext, err)
		}
		if got != plaintext {
			t.Errorf("roundtrip = %q, want %q", got, plaintext)
		}
	}

	// Fresh nonces: encrypting the same value twice must differ.
	a, _ := Encrypt(key, "same input")
	b, _ := Encrypt(key, "same input")
	if a == b {
		t.Error("two Encrypt calls produced identical ciphertext (nonce reuse?)")
	}
}

func TestDecryptRejectsBadInput(t *testing.T) {
	key, err := LoadOrCreateSecret(filepath.Join(t.TempDir(), "secret"))
	if err != nil {
		t.Fatalf("LoadOrCreateSecret: %v", err)
	}
	sealed, err := Encrypt(key, "top secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Tampered ciphertext: flip the last byte.
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}
	raw[len(raw)-1] ^= 0xff
	if _, err := Decrypt(key, base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Error("Decrypt accepted tampered ciphertext")
	}

	// Wrong key.
	other, err := LoadOrCreateSecret(filepath.Join(t.TempDir(), "secret"))
	if err != nil {
		t.Fatalf("LoadOrCreateSecret (other): %v", err)
	}
	if _, err := Decrypt(other, sealed); err == nil {
		t.Error("Decrypt accepted ciphertext under the wrong key")
	}

	// Truncated below the nonce size.
	short := base64.StdEncoding.EncodeToString([]byte("abc"))
	if _, err := Decrypt(key, short); err == nil {
		t.Error("Decrypt accepted ciphertext shorter than the nonce")
	} else if !strings.Contains(err.Error(), "too short") {
		t.Errorf("error = %q, want mention of too short", err)
	}

	// Not base64 at all.
	if _, err := Decrypt(key, "%%% not base64 %%%"); err == nil {
		t.Error("Decrypt accepted invalid base64")
	}
}
