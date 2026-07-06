// Package mailstore knows the on-disk Maildir layout. wispboxd never reads
// mail through the filesystem (IMAP is the access path); this package exists
// for provisioning, quota reporting, and backups.
package mailstore

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MaildirPath returns the Maildir root for a mailbox address:
//
//	<mailDir>/<domain>/<localpart>/Maildir
func MaildirPath(mailDir, email string) (string, error) {
	local, domain, ok := strings.Cut(strings.ToLower(email), "@")
	if !ok || local == "" || domain == "" {
		return "", fmt.Errorf("malformed address %q", email)
	}
	if strings.ContainsAny(local+domain, "/\\") || strings.Contains(email, "..") {
		return "", fmt.Errorf("unsafe address %q", email)
	}
	return filepath.Join(mailDir, domain, local, "Maildir"), nil
}

// EnsureMaildir creates the Maildir skeleton (cur/new/tmp) for a mailbox.
// Dovecot would create it on first delivery anyway; doing it at mailbox
// creation makes IMAP logins work immediately.
func EnsureMaildir(mailDir, email string) error {
	root, err := MaildirPath(mailDir, email)
	if err != nil {
		return err
	}
	for _, sub := range []string{"cur", "new", "tmp"} {
		if err := os.MkdirAll(filepath.Join(root, sub), 0o700); err != nil {
			return err
		}
	}
	return nil
}

// DiskUsage sums bytes under a mailbox's Maildir. Returns 0 for a mailbox
// that has not received mail yet.
func DiskUsage(mailDir, email string) (int64, error) {
	root, err := MaildirPath(mailDir, email)
	if err != nil {
		return 0, err
	}
	var total int64
	err = filepath.WalkDir(root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries rather than failing the report
		}
		if info, err := d.Info(); err == nil && !d.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if os.IsNotExist(err) {
		return 0, nil
	}
	return total, nil
}
