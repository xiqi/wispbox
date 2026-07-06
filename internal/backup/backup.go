// Package backup creates and restores wispbox backups: the control database
// (snapshotted safely with VACUUM INTO), certificates, DKIM keys, and the
// instance secret. Mail storage is intentionally separate — Maildir is plain
// files and users typically rsync it on its own schedule.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xiqi/wispbox/internal/config"
)

// Create writes a .tar.gz backup to outPath and returns the bytes written.
func Create(ctx context.Context, cfg *config.Config, sqldb *sql.DB, outPath string) (int64, error) {
	tmpDir, err := os.MkdirTemp(filepath.Dir(outPath), ".wispbox-backup-*")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmpDir)

	// VACUUM INTO produces a consistent snapshot even while the daemon runs.
	snapshot := filepath.Join(tmpDir, "wispbox.db")
	if _, err := sqldb.ExecContext(ctx, `VACUUM INTO ?`, snapshot); err != nil {
		return 0, fmt.Errorf("snapshot database: %w", err)
	}

	out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	defer out.Close()

	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)

	addFile := func(diskPath, archivePath string) error {
		info, err := os.Stat(diskPath)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = archivePath
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(diskPath)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	}

	addTree := func(root, prefix string) error {
		return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			return addFile(p, filepath.ToSlash(filepath.Join(prefix, rel)))
		})
	}

	if err := addFile(snapshot, "wispbox.db"); err != nil {
		return 0, fmt.Errorf("archive database: %w", err)
	}
	if _, err := os.Stat(cfg.SecretPath); err == nil {
		if err := addFile(cfg.SecretPath, "secret.key"); err != nil {
			return 0, fmt.Errorf("archive secret: %w", err)
		}
	}
	if err := addTree(cfg.CertDir, "certs"); err != nil {
		return 0, fmt.Errorf("archive certificates: %w", err)
	}
	if err := addTree(cfg.DKIMDir, "dkim"); err != nil {
		return 0, fmt.Errorf("archive DKIM keys: %w", err)
	}

	meta := fmt.Sprintf("created_at=%s\nmode=%s\n", time.Now().UTC().Format(time.RFC3339), cfg.Mode)
	if err := tw.WriteHeader(&tar.Header{Name: "backup-info.txt", Mode: 0o600, Size: int64(len(meta)), ModTime: time.Now()}); err != nil {
		return 0, err
	}
	if _, err := io.WriteString(tw, meta); err != nil {
		return 0, err
	}

	if err := tw.Close(); err != nil {
		return 0, err
	}
	if err := gz.Close(); err != nil {
		return 0, err
	}
	info, err := out.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// Restore unpacks a backup into the configured data layout. The daemon must
// be stopped first; Restore refuses to run if the DB file is newer than the
// backup unless force is set.
func Restore(_ context.Context, cfg *config.Config, inPath string, force bool) error {
	f, err := os.Open(inPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s is not a wispbox backup (gzip): %w", inPath, err)
	}
	tr := tar.NewReader(gz)

	if !force {
		if info, err := os.Stat(cfg.DBPath); err == nil {
			if backupInfo, err2 := os.Stat(inPath); err2 == nil && info.ModTime().After(backupInfo.ModTime()) {
				return fmt.Errorf("existing database at %s is newer than the backup; pass --force to overwrite", cfg.DBPath)
			}
		}
	}

	// A live database may have -wal/-shm sidecars. If we drop a restored
	// wispbox.db next to a stale WAL, SQLite replays the old journal and
	// silently reverts (or corrupts) the restore. Remove them first.
	for _, sfx := range []string{"-wal", "-shm"} {
		if err := os.Remove(cfg.DBPath + sfx); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale %s: %w", cfg.DBPath+sfx, err)
		}
	}

	restoreTargets := map[string]string{
		"wispbox.db": cfg.DBPath,
		"secret.key": cfg.SecretPath,
	}

	// When restoring as root (production), created files and dirs must end up
	// owned by the wispbox user or the daemon, Dovecot, and OpenDKIM cannot
	// read them. In development (non-root), keep the current user.
	uid, gid := -1, -1
	if os.Geteuid() == 0 {
		if u, err := user.Lookup("wispbox"); err == nil {
			uid, _ = strconv.Atoi(u.Uid)
			gid, _ = strconv.Atoi(u.Gid)
		}
	}
	chownIfRoot := func(path string) {
		if uid >= 0 {
			_ = os.Chown(path, uid, gid)
		}
	}

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(hdr.Name)
		if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("backup contains unsafe path %q; refusing to restore", hdr.Name)
		}

		var target string
		switch {
		case restoreTargets[name] != "":
			target = restoreTargets[name]
		case strings.HasPrefix(name, "certs/"):
			target = filepath.Join(cfg.CertDir, strings.TrimPrefix(name, "certs/"))
		case strings.HasPrefix(name, "dkim/"):
			target = filepath.Join(cfg.DKIMDir, strings.TrimPrefix(name, "dkim/"))
		case name == "backup-info.txt":
			continue
		default:
			continue // unknown entries are skipped, not written
		}

		// Restore the archived permission bits; secrets and private keys were
		// stored 0600/0640, everything else no wider than the tar recorded.
		mode := os.FileMode(hdr.Mode).Perm()
		if mode == 0 {
			mode = 0o640
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		chownIfRoot(filepath.Dir(target))
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, 1<<30)); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		if err := os.Chmod(target, mode); err != nil {
			return err
		}
		chownIfRoot(target)
	}
	return nil
}
