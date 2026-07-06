package configgen

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// EnsureDKIMKey generates a 2048-bit RSA DKIM keypair for a domain if one
// does not exist yet. Returns the DNS TXT record value.
func EnsureDKIMKey(dkimDir, domain string) (txtValue string, err error) {
	keyPath := filepath.Join(dkimDir, domain, DKIMSelector+".private")
	if _, err := os.Stat(keyPath); err == nil {
		return DKIMTXTValue(dkimDir, domain)
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o750); err != nil {
		return "", err
	}
	// 0640: the opendkim system user reads keys via wispbox group membership.
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(keyPath, keyPEM, 0o640); err != nil {
		return "", err
	}
	return DKIMTXTValue(dkimDir, domain)
}

// DKIMTXTValue reads a domain's private key and derives the public TXT value.
func DKIMTXTValue(dkimDir, domain string) (string, error) {
	keyPath := filepath.Join(dkimDir, domain, DKIMSelector+".private")
	raw, err := os.ReadFile(keyPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", fmt.Errorf("corrupt DKIM key for %s", domain)
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse DKIM key for %s: %w", domain, err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	return "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(pubDER), nil
}
