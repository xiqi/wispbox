// Package auth implements password hashing, sessions, and CSRF tokens.
//
// Two separate credential planes exist by design:
//
//   - Admin accounts: argon2id hashes, verified only by wispboxd.
//   - Mailbox accounts: bcrypt hashes stored with a {BLF-CRYPT} prefix so the
//     exact same database row is usable by Dovecot's SQL passdb and by the
//     Webmail login path in wispboxd.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Argon2id parameters follow OWASP guidance while staying friendly to
// 512MB hosts (19 MiB per hash, single lane).
const (
	argonTime    = 2
	argonMemory  = 19456 // KiB
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashAdminPassword hashes an admin password with argon2id (PHC string).
func HashAdminPassword(password string) (string, error) {
	if err := CheckPasswordStrength(password); err != nil {
		return "", err
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyAdminPassword checks a password against a PHC argon2id hash.
func VerifyAdminPassword(password, phc string) bool {
	parts := strings.Split(phc, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// mailboxHashPrefix is the Dovecot password scheme marker. Debian and Ubuntu
// builds of Dovecot support BLF-CRYPT (bcrypt) out of the box via libxcrypt.
const mailboxHashPrefix = "{BLF-CRYPT}"

// HashMailboxPassword hashes a mailbox password with bcrypt in a
// Dovecot-compatible form.
func HashMailboxPassword(password string) (string, error) {
	if err := CheckPasswordStrength(password); err != nil {
		return "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}
	return mailboxHashPrefix + string(h), nil
}

// VerifyMailboxPassword checks a password against a {BLF-CRYPT} bcrypt hash.
func VerifyMailboxPassword(password, stored string) bool {
	h := strings.TrimPrefix(stored, mailboxHashPrefix)
	return bcrypt.CompareHashAndPassword([]byte(h), []byte(password)) == nil
}

// CheckPasswordStrength enforces the minimal bar for any account password.
func CheckPasswordStrength(password string) error {
	if len(password) < 10 {
		return fmt.Errorf("password must be at least 10 characters")
	}
	if len(password) > 256 {
		return fmt.Errorf("password is too long")
	}
	return nil
}

// GeneratePassword returns a random URL-safe password (for reset flows).
func GeneratePassword() string {
	raw := make([]byte, 18)
	if _, err := rand.Read(raw); err != nil {
		panic(err) // crypto/rand failure is unrecoverable
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
