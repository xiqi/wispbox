// Package cli implements the commands shared by wispboxd and wispboxctl.
// Every command here operates on the local configuration and database; the
// only commands that touch host services are the ones a user explicitly
// runs on their own server (and never in development mode).
package cli

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"time"

	"github.com/xiqi/wispbox/internal/api"
	"github.com/xiqi/wispbox/internal/buildinfo"
	"github.com/xiqi/wispbox/internal/config"
	"github.com/xiqi/wispbox/internal/db"
)

// Options are the global flags shared by both binaries.
type Options struct {
	ConfigPath string
	Dev        bool
	DevDir     string
	Seed       bool
	HTTPAddr   string
	HTTPSAddr  string
}

// RegisterFlags wires the flags common to every command (both binaries) onto
// a FlagSet.
func (o *Options) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&o.ConfigPath, "config", "/etc/wispbox/wispbox.conf", "path to wispbox.conf")
	fs.BoolVar(&o.Dev, "dev", false, "development mode: high ports, mock adapters, local data dir")
	fs.StringVar(&o.DevDir, "dev-dir", "./devdata", "data directory used in development mode")
}

// RegisterServeFlags wires the flags that only apply to `wispboxd serve`, so
// other commands don't advertise (or silently accept) knobs they never use.
func (o *Options) RegisterServeFlags(fs *flag.FlagSet) {
	fs.BoolVar(&o.Seed, "seed", true, "in development mode, seed demo data on first run")
	fs.StringVar(&o.HTTPAddr, "http", "", "override HTTP listen address")
	fs.StringVar(&o.HTTPSAddr, "https", "", "override HTTPS listen address")
}

// Load resolves the effective configuration from flags + config file.
func (o *Options) Load() (*config.Config, error) {
	defaults := config.ProductionDefaults()
	if o.Dev {
		defaults = config.DevelopmentDefaults(o.DevDir)
	}
	cfg, err := config.Load(o.ConfigPath, defaults)
	if err != nil {
		return nil, err
	}
	if o.Dev {
		cfg.Mode = config.ModeDevelopment // --dev always wins over file
	}
	if o.HTTPAddr != "" {
		cfg.HTTPAddr = o.HTTPAddr
	}
	if o.HTTPSAddr != "" {
		cfg.HTTPSAddr = o.HTTPSAddr
	}
	return cfg, nil
}

// newApp builds the daemon composition for CLI commands.
func (o *Options) newApp(ctx context.Context) (*api.App, error) {
	cfg, err := o.Load()
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return api.NewApp(ctx, cfg, logger)
}

// Serve runs the daemon until interrupted.
func Serve(ctx context.Context, o *Options) error {
	app, err := o.newApp(ctx)
	if err != nil {
		return err
	}
	defer app.Close()
	if app.Cfg.IsDev() && o.Seed {
		if err := app.SeedDev(ctx); err != nil {
			return fmt.Errorf("seed dev data: %w", err)
		}
	}
	mode := "production"
	if app.Cfg.IsDev() {
		mode = "development (mock adapters, no host changes)"
	}
	app.Log.Info("wispboxd starting",
		"version", buildinfo.Version, "mode", mode,
		"http", app.Cfg.HTTPAddr, "https", app.Cfg.HTTPSAddr, "db", app.Cfg.DBPath)
	if app.Cfg.IsDev() {
		fmt.Fprintf(os.Stderr, "\n  Webmail:  http://localhost%s/\n  Admin:    http://localhost%s/admin\n  Setup:    http://localhost%s/setup\n\n",
			app.Cfg.HTTPAddr, app.Cfg.HTTPAddr, app.Cfg.HTTPAddr)
	}
	return app.Serve(ctx)
}

// Migrate applies pending migrations and exits.
func Migrate(ctx context.Context, o *Options) error {
	cfg, err := o.Load()
	if err != nil {
		return err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return err
	}
	sqldb, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer sqldb.Close()
	applied, err := db.Migrate(ctx, sqldb)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		fmt.Println("database is up to date")
	} else {
		for _, name := range applied {
			fmt.Println("applied", name)
		}
	}
	return nil
}

// ConfigRender renders all generated config. With write=true the files land
// in the generated directory (no services are reloaded); otherwise contents
// go to stdout.
func ConfigRender(ctx context.Context, o *Options, write bool) error {
	app, err := o.newApp(ctx)
	if err != nil {
		return err
	}
	defer app.Close()
	_, files, err := app.Generator.RenderAll(ctx)
	if err != nil {
		return err
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if !write {
		for _, p := range paths {
			fmt.Printf("# ===== %s =====\n%s\n", p, files[p])
		}
		return nil
	}
	prev := app.Generator.ReloadServices
	app.Generator.ReloadServices = false
	defer func() { app.Generator.ReloadServices = prev }()
	if err := app.Generator.Apply(ctx); err != nil {
		return err
	}
	for _, p := range paths {
		fmt.Println("wrote", p)
	}
	return nil
}

// ConfigValidate renders everything in memory and reports success.
func ConfigValidate(ctx context.Context, o *Options) error {
	app, err := o.newApp(ctx)
	if err != nil {
		return err
	}
	defer app.Close()
	_, files, err := app.Generator.RenderAll(ctx)
	if err != nil {
		return fmt.Errorf("configuration is INVALID: %w", err)
	}
	fmt.Printf("configuration is valid (%d files render cleanly)\n", len(files))
	return nil
}

// CertCheck prints certificate state from the database.
func CertCheck(ctx context.Context, o *Options) error {
	app, err := o.newApp(ctx)
	if err != nil {
		return err
	}
	defer app.Close()
	certList, err := app.Store.ListCertificates(ctx)
	if err != nil {
		return err
	}
	if len(certList) == 0 {
		fmt.Println("no certificates tracked yet — add a domain first")
		return nil
	}
	for _, c := range certList {
		line := fmt.Sprintf("%-40s %-9s", c.Hostname, c.Status)
		if exp, ok := c.NotAfterTime(); ok {
			line += fmt.Sprintf(" expires %s (%dd)", exp.Format("2006-01-02"), int(time.Until(exp).Hours()/24))
		}
		if c.LastError != "" {
			line += "\n    last error: " + c.LastError
		}
		fmt.Println(line)
	}
	return nil
}

// CertRenew marks certificates due immediately. The running daemon picks
// them up within its renewal loop; with the daemon stopped, the next start
// renews them. hostname == "" means all.
func CertRenew(ctx context.Context, o *Options, hostname string) error {
	app, err := o.newApp(ctx)
	if err != nil {
		return err
	}
	defer app.Close()
	certList, err := app.Store.ListCertificates(ctx)
	if err != nil {
		return err
	}
	count := 0
	for _, c := range certList {
		if hostname != "" && c.Hostname != hostname {
			continue
		}
		if err := app.Store.SetCertificateRenewAfter(ctx, c.ID, time.Now()); err != nil {
			return err
		}
		fmt.Println("renewal scheduled for", c.Hostname)
		count++
	}
	if count == 0 {
		return fmt.Errorf("no tracked certificate matches %q", hostname)
	}
	fmt.Println("the running wispboxd will renew within a minute; if it is stopped, renewal happens on next start")
	return nil
}

// Version prints build information.
func Version() {
	fmt.Println("wispbox", buildinfo.String())
}
