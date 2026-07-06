package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/xiqi/wispbox/internal/db"
)

type check struct {
	name   string
	ok     bool
	detail string
}

// Doctor runs read-only environment diagnostics. It never modifies the host.
func Doctor(ctx context.Context, o *Options) error {
	cfg, err := o.Load()
	if err != nil {
		return err
	}
	var checks []check
	add := func(name string, ok bool, detail string) {
		checks = append(checks, check{name, ok, detail})
	}

	add("mode", true, string(cfg.Mode))

	for _, dir := range []string{cfg.ConfigDir, cfg.GeneratedDir, cfg.DataDir, cfg.CertDir, cfg.MailDir, cfg.LogDir} {
		if info, err := os.Stat(dir); err != nil {
			add("dir "+dir, false, "missing — run the installer or `wispboxd migrate` first")
		} else if !info.IsDir() {
			add("dir "+dir, false, "exists but is not a directory")
		} else {
			add("dir "+dir, true, "ok")
		}
	}

	if _, err := os.Stat(cfg.DBPath); err != nil {
		add("database", false, cfg.DBPath+" does not exist yet — run `wispboxd migrate`")
	} else if sqldb, err := db.Open(cfg.DBPath); err != nil {
		add("database", false, err.Error())
	} else {
		store := db.NewStore(sqldb)
		domains, derr := store.ListDomains(ctx)
		if derr != nil {
			add("database", false, "schema incomplete — run `wispboxd migrate`: "+derr.Error())
		} else {
			add("database", true, fmt.Sprintf("ok (%d domains)", len(domains)))
			if store.IsInitialized(ctx) {
				add("setup", true, "initialization complete")
			} else {
				add("setup", false, "first-run setup not completed — open /setup in a browser")
			}
			certs, _ := store.ListCertificates(ctx)
			bad := 0
			for _, c := range certs {
				if c.Status == db.CertError {
					bad++
				}
			}
			add("certificates", bad == 0, fmt.Sprintf("%d tracked, %d failing", len(certs), bad))
		}
		sqldb.Close()
	}

	if cfg.IsDev() {
		add("mail services", true, "development mode: Postfix/Dovecot are mocked, host untouched")
	} else {
		for _, bin := range []string{"postfix", "dovecot", "postqueue"} {
			if _, err := exec.LookPath(bin); err != nil {
				add(bin, false, "not found on PATH — run the wispbox installer")
			} else {
				add(bin, true, "installed")
			}
		}
		if _, err := exec.LookPath("opendkim"); err != nil {
			add("opendkim", false, "not installed — outbound mail will not be DKIM-signed (optional but recommended)")
		} else {
			add("opendkim", true, "installed")
		}
	}

	if _, err := os.Stat(filepath.Join(cfg.CertDir, "_default", "fullchain.pem")); err == nil {
		add("fallback certificate", true, "present")
	} else {
		add("fallback certificate", false, "not created yet — generated on first `wispboxd serve`")
	}

	failures := 0
	for _, c := range checks {
		mark := "✓"
		if !c.ok {
			mark = "✗"
			failures++
		}
		fmt.Printf("  %s %-40s %s\n", mark, c.name, c.detail)
	}
	if failures > 0 {
		return fmt.Errorf("%d check(s) need attention", failures)
	}
	fmt.Println("\neverything looks good")
	return nil
}

// Status is wispboxctl's quick overview.
func Status(ctx context.Context, o *Options) error {
	app, err := o.newApp(ctx)
	if err != nil {
		return err
	}
	defer app.Close()

	fmt.Println("mode:      ", app.Cfg.Mode)
	fmt.Println("database:  ", app.Cfg.DBPath)
	fmt.Println("initialized:", app.Store.IsInitialized(ctx))

	domains, _ := app.Store.ListDomains(ctx)
	mailboxes, _ := app.Store.ListMailboxes(ctx, 0)
	aliases, _ := app.Store.ListAliases(ctx, 0)
	fmt.Printf("objects:    %d domain(s), %d mailbox(es), %d alias(es)\n", len(domains), len(mailboxes), len(aliases))

	for _, svc := range []string{"wispboxd", "postfix", "dovecot"} {
		active, err := app.Services.IsActive(ctx, svc)
		state := "active"
		if err != nil {
			state = "unknown: " + err.Error()
		} else if !active {
			state = "inactive"
		}
		fmt.Printf("service:    %-9s %s\n", svc, state)
	}
	if n, err := app.Queue.Count(ctx); err == nil {
		fmt.Println("queue:     ", n, "message(s)")
	}
	return nil
}
