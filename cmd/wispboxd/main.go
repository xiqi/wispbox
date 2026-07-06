// wispboxd is the wispbox daemon: HTTPS server, webmail/admin/setup UIs,
// REST APIs, certificate automation, and mail server config generation.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/xiqi/wispbox/internal/cli"
	"github.com/xiqi/wispbox/internal/lowmem"
)

const usage = `wispboxd — the wispbox mail server daemon

Usage:
  wispboxd serve            run the daemon
  wispboxd migrate          apply database migrations
  wispboxd config render    render Postfix/Dovecot config (add --write to save)
  wispboxd config validate  check that config renders cleanly
  wispboxd cert check       show certificate status
  wispboxd cert renew       schedule certificate renewal [--hostname mail.example.com]
  wispboxd doctor           diagnose the installation (read-only)
  wispboxd version          print version

Common flags (after the subcommand):
  --config PATH   config file (default /etc/wispbox/wispbox.conf)
  --dev           development mode: mock adapters, high ports, ./devdata
  --dev-dir DIR   development data directory (default ./devdata)
`

func main() {
	lowmem.ApplyDefaults()
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	cmd := args[0]
	rest := args[1:]

	// `config render` / `cert renew` style two-word commands.
	sub := ""
	if (cmd == "config" || cmd == "cert" || cmd == "backup") && len(rest) > 0 {
		sub = rest[0]
		rest = rest[1:]
	}

	opts := &cli.Options{}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	opts.RegisterFlags(fs)
	if cmd == "serve" {
		opts.RegisterServeFlags(fs)
	}
	write := fs.Bool("write", false, "write rendered config to the generated directory")
	hostname := fs.String("hostname", "", "limit cert renew to one hostname")
	if err := fs.Parse(rest); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "serve":
		return cli.Serve(ctx, opts)
	case "migrate":
		return cli.Migrate(ctx, opts)
	case "config":
		switch sub {
		case "render":
			return cli.ConfigRender(ctx, opts, *write)
		case "validate":
			return cli.ConfigValidate(ctx, opts)
		}
		return fmt.Errorf("unknown config subcommand %q (want render or validate)", sub)
	case "cert":
		switch sub {
		case "check":
			return cli.CertCheck(ctx, opts)
		case "renew":
			return cli.CertRenew(ctx, opts, *hostname)
		}
		return fmt.Errorf("unknown cert subcommand %q (want check or renew)", sub)
	case "doctor":
		return cli.Doctor(ctx, opts)
	case "version":
		cli.Version()
		return nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
}
