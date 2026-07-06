package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/xiqi/wispbox/internal/backup"
	"github.com/xiqi/wispbox/internal/db"
)

// BackupCreate writes a backup archive. Default path includes a timestamp.
func BackupCreate(ctx context.Context, o *Options, outPath string) error {
	cfg, err := o.Load()
	if err != nil {
		return err
	}
	if outPath == "" {
		outPath = fmt.Sprintf("wispbox-backup-%s.tar.gz", time.Now().Format("20060102-150405"))
	}
	sqldb, err := db.Open(cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open database (has wispboxd ever run here?): %w", err)
	}
	defer sqldb.Close()
	size, err := backup.Create(ctx, cfg, sqldb, outPath)
	if err != nil {
		return err
	}
	fmt.Printf("backup written to %s (%.1f KiB)\n", outPath, float64(size)/1024)
	fmt.Println("note: Maildir mail storage is not included; back up", cfg.MailDir, "separately (plain files, rsync-friendly)")
	return nil
}

// BackupRestore restores an archive into the configured layout.
func BackupRestore(ctx context.Context, o *Options, inPath string, force bool) error {
	cfg, err := o.Load()
	if err != nil {
		return err
	}
	if inPath == "" {
		return fmt.Errorf("usage: wispboxctl backup restore <archive.tar.gz>")
	}
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	if err := backup.Restore(ctx, cfg, inPath, force); err != nil {
		return err
	}
	fmt.Println("backup restored — restart wispboxd to pick it up (systemctl restart wispboxd)")
	return nil
}
