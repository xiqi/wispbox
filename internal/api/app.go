// Package api wires every wispbox component together and runs the HTTP and
// HTTPS servers. Adapter selection (production vs development mocks) happens
// here and only here.
package api

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/xiqi/wispbox/internal/acme"
	"github.com/xiqi/wispbox/internal/admin"
	"github.com/xiqi/wispbox/internal/auth"
	"github.com/xiqi/wispbox/internal/certs"
	"github.com/xiqi/wispbox/internal/config"
	"github.com/xiqi/wispbox/internal/configgen"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/delivery"
	"github.com/xiqi/wispbox/internal/dnscheck"
	"github.com/xiqi/wispbox/internal/imapclient"
	"github.com/xiqi/wispbox/internal/mailapi"
	"github.com/xiqi/wispbox/internal/netcheck"
	"github.com/xiqi/wispbox/internal/security"
	"github.com/xiqi/wispbox/internal/services"
	"github.com/xiqi/wispbox/internal/setup"
	"github.com/xiqi/wispbox/internal/smtpclient"
)

// App is the assembled wispbox daemon.
type App struct {
	Cfg      *config.Config
	Log      *slog.Logger
	DB       *sql.DB
	Store    *db.Store
	Secret   []byte
	Sessions *auth.Sessions

	Solver      *acme.HTTP01Solver
	CertManager *certs.Manager
	Checker     *dnscheck.Checker
	Resolver    dnscheck.Resolver
	Engine      *delivery.Engine
	Generator   *configgen.Generator
	Services    services.Manager
	Queue       services.QueueInspector
	Logs        services.LogReader
	IMAP        imapclient.Client
	SMTP        smtpclient.Sender
	TestMailer  admin.TestMailer

	AdminH *admin.Handlers
	MailH  *mailapi.Handlers
	SetupH *setup.Handlers

	StartedAt time.Time
}

const (
	devSeedAdminUsername = "admin"
	devSeedAdminPassword = "wispbox-admin"
	devSeedMailboxEmail  = "demo@example.com"
	devSeedMailboxLocal  = "demo"
	devSeedMailboxPass   = "wispbox-demo"
	devSeedDomain        = "example.com"
)

// NewApp builds the daemon for the given configuration. It creates
// directories, opens and migrates the database, and selects production or
// mock adapters based on cfg.Mode.
func NewApp(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := cfg.EnsureDirs(); err != nil {
		return nil, err
	}

	sqldb, err := db.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}
	applied, err := db.Migrate(ctx, sqldb)
	if err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	if len(applied) > 0 {
		logger.Info("database migrations applied", "count", len(applied))
	}
	store := db.NewStore(sqldb)

	secret, err := security.LoadOrCreateSecret(cfg.SecretPath)
	if err != nil {
		sqldb.Close()
		return nil, err
	}

	app := &App{
		Cfg:       cfg,
		Log:       logger,
		DB:        sqldb,
		Store:     store,
		Secret:    secret,
		Sessions:  auth.NewSessions(store, secret, !cfg.IsDev()),
		Solver:    acme.NewHTTP01Solver(),
		Engine:    delivery.NewEngine(store),
		StartedAt: time.Now(),
	}

	// ---- adapter selection ----
	if cfg.IsDev() {
		mockResolver := dnscheck.NewMockResolver()
		app.Resolver = mockResolver
		app.Services = services.NewMockManager(store)
		app.Queue = services.NewMockQueue()
		app.Logs = services.NewMockLogReader()
		imapMock := imapclient.NewMock()
		app.IMAP = imapMock
		app.SMTP = smtpclient.NewMockSender(imapMock)
		app.TestMailer = &admin.MockTestMailer{}
	} else {
		app.Resolver = dnscheck.NewNetResolver()
		app.Services = services.NewSystemdManager(store)
		app.Queue = services.NewPostfixQueue()
		app.Logs = services.NewJournalLogReader()
		app.IMAP = imapclient.NewDovecot("127.0.0.1:143")
		app.TestMailer = &admin.SMTPTestMailer{}
	}
	app.Checker = dnscheck.NewChecker(app.Resolver)

	// Certificate issuance: mock/self-signed in dev, Let's Encrypt in prod.
	var issuer acme.Issuer
	if cfg.IsDev() {
		issuer = acme.NewSelfSignedIssuer()
	} else {
		dir := cfg.ACMEDirectoryURL
		if dir == "" {
			dir = config.LetsEncryptProduction
		}
		issuer = acme.NewLetsEncryptIssuer(dir,
			func() string {
				// Read the contact at registration time; setup collects it
				// before the first certificate is issued.
				return store.GetSettingDefault(context.Background(), "acme_email", cfg.ACMEEmail)
			},
			filepath.Join(cfg.CertDir, "acme-account.key"), app.Solver)
	}
	app.CertManager = certs.NewManager(cfg.CertDir, store, issuer, app.Checker, app.Services, logger)

	// Config generation.
	mailUser, mailGroup := "wispbox", "wispbox"
	if cfg.IsDev() {
		mailUser, mailGroup = "wispbox-dev", "wispbox-dev" // never applied to a host in dev
	}
	// Pick the Dovecot config dialect from the installed version. Debian 13
	// and Ubuntu 26.04 ship Dovecot 2.4, whose config format differs from
	// 2.3 (Debian 12, Ubuntu 24.04). Dev mode never touches a real Dovecot,
	// so it stays on the 2.3 templates by default.
	dovecotV24 := false
	if !cfg.IsDev() {
		if configgen.DovecotIsV24Plus() {
			dovecotV24 = true
			logger.Info("detected Dovecot 2.4+, using 2.4 config templates")
		}
	}
	app.Generator = &configgen.Generator{
		Builder: &configgen.Builder{
			Cfg: cfg, Store: store, Engine: app.Engine, Secret: secret,
			MailUser: mailUser, MailGroup: mailGroup, DovecotV24: dovecotV24,
		},
		Services:       app.Services,
		Store:          store,
		Log:            logger,
		ReloadServices: !cfg.IsDev(), // dev writes files, never touches services
	}

	// Production SMTP submission needs the primary hostname for EHLO.
	if !cfg.IsDev() {
		app.SMTP = smtpclient.NewSubmission("127.0.0.1:587",
			store.GetSettingDefault(ctx, "primary_hostname", "localhost"))
	}

	core := &admin.Core{
		Cfg: cfg, Store: store, Engine: app.Engine, Generator: app.Generator,
		Certs: app.CertManager, Checker: app.Checker, Secret: secret, Log: logger,
		OutboundSMTP25Open: netcheck.OutboundSMTP25Open,
	}
	app.CertManager.ServerIPs = core.ServerIPs
	// After a certificate is issued, regenerate the mail config so the new
	// cert lands in the Postfix SNI map and Dovecot local_name blocks. In
	// development the generator writes files but never touches services.
	gen := app.Generator
	app.CertManager.OnIssued = func(ctx context.Context) error { return gen.Apply(ctx) }

	loginLimiter := security.NewRateLimiter(0.2, 5) // 5 tries, then 1 per 5s per IP

	app.AdminH = &admin.Handlers{
		Core: core, Sessions: app.Sessions, LoginLimiter: loginLimiter,
		Services: app.Services, Queue: app.Queue, Logs: app.Logs,
		TestMailer: app.TestMailer, StartedAt: app.StartedAt,
	}
	app.MailH = &mailapi.Handlers{
		Store: store, Sessions: app.Sessions, Secret: app.Secret, IMAP: app.IMAP, SMTP: app.SMTP,
		LoginLimiter: loginLimiter, Log: logger,
	}
	app.SetupH = &setup.Handlers{
		Cfg: cfg, Core: core, Sessions: app.Sessions,
		LoginLimiter: loginLimiter, TestMailer: app.TestMailer,
	}
	return app, nil
}

// Close releases resources.
func (a *App) Close() error { return a.DB.Close() }

// SeedDev populates a fresh development database with a demo admin, domain,
// and mailbox, and marks setup complete so the webmail is immediately usable.
// No-op if data already exists.
func (a *App) SeedDev(ctx context.Context) error {
	if !a.Cfg.IsDev() {
		return fmt.Errorf("seeding is a development-mode feature")
	}
	if n, _ := a.Store.CountAdmins(ctx); n > 0 {
		return nil
	}
	a.Log.Info("seeding development data",
		"admin", devSeedAdminUsername+" / "+devSeedAdminPassword,
		"mailbox", devSeedMailboxEmail+" / "+devSeedMailboxPass)

	hash, err := auth.HashAdminPassword(devSeedAdminPassword)
	if err != nil {
		return err
	}
	if _, err := a.Store.CreateAdmin(ctx, devSeedAdminUsername, hash); err != nil {
		return err
	}
	for k, v := range map[string]string{
		"primary_hostname": "mail.example.com",
		"server_ipv4":      "203.0.113.10",
		"acme_email":       "hostmaster@example.com",
		"initialized":      "true",
	} {
		if err := a.Store.SetSetting(ctx, k, v); err != nil {
			return err
		}
	}
	core := a.AdminH.Core
	dom, _, err := core.CreateDomain(ctx, devSeedDomain, "")
	if err != nil {
		return err
	}
	if _, _, err := core.CreateMailbox(ctx, dom.ID, devSeedMailboxLocal, devSeedMailboxPass, 0); err != nil {
		return err
	}
	if mock, ok := a.Resolver.(*dnscheck.MockResolver); ok {
		txt, _ := configgen.DKIMTXTValue(a.Cfg.DKIMDir, dom.Name)
		mock.SeedHappyDomain(dom.Name, dom.MailHostname, "203.0.113.10", configgen.DKIMSelector, txt)
	}
	return nil
}
