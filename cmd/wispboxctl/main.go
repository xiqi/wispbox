// wispboxctl is the wispbox operations CLI: status, diagnostics, config
// rendering, certificate renewal, and backups.
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

const usage = `wispboxctl — wispbox operations tool

Usage:
  wispboxctl status                  show services, database, and queue state
  wispboxctl doctor                  diagnose the installation (read-only)
  wispboxctl config render           render Postfix/Dovecot config (--write to save)
  wispboxctl config validate         check that config renders cleanly
  wispboxctl cert renew              schedule certificate renewal [--hostname H]
  wispboxctl backup create [PATH]    write a backup archive
  wispboxctl backup restore PATH     restore a backup archive [--force]

Common flags:
  --config PATH   config file (default /etc/wispbox/wispbox.conf)
  --dev           development mode: mock adapters, ./devdata
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

	sub := ""
	if (cmd == "config" || cmd == "cert" || cmd == "backup") && len(rest) > 0 {
		sub = rest[0]
		rest = rest[1:]
	}

	// Only backup subcommands take a positional path; grab it if present.
	// Other commands reject stray positionals below so they can't be
	// silently ignored (e.g. `cert renew mail.example.com`).
	pathArg := ""
	if cmd == "backup" && len(rest) > 0 && rest[0] != "" && rest[0][0] != '-' {
		pathArg = rest[0]
		rest = rest[1:]
	}

	opts := &cli.Options{}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	opts.RegisterFlags(fs)
	write := fs.Bool("write", false, "write rendered config to the generated directory")
	hostname := fs.String("hostname", "", "limit cert renew to one hostname")
	force := fs.Bool("force", false, "overwrite newer local data on restore")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		if cmd == "backup" && pathArg == "" && fs.NArg() == 1 {
			pathArg = fs.Arg(0)
		} else {
			return fmt.Errorf("unexpected argument %q; run `wispboxctl help`. (Tip: `cert renew` targets one host with --hostname mail.example.com)", fs.Arg(0))
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "status":
		return cli.Status(ctx, opts)
	case "doctor":
		return cli.Doctor(ctx, opts)
	case "config":
		switch sub {
		case "render":
			return cli.ConfigRender(ctx, opts, *write)
		case "validate":
			return cli.ConfigValidate(ctx, opts)
		}
		return fmt.Errorf("unknown config subcommand %q (want render or validate)", sub)
	case "cert":
		if sub == "renew" {
			return cli.CertRenew(ctx, opts, *hostname)
		}
		return fmt.Errorf("unknown cert subcommand %q (want renew)", sub)
	case "backup":
		switch sub {
		case "create":
			return cli.BackupCreate(ctx, opts, pathArg)
		case "restore":
			return cli.BackupRestore(ctx, opts, pathArg, *force)
		}
		return fmt.Errorf("unknown backup subcommand %q (want create or restore)", sub)
	case "version":
		cli.Version()
		return nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n\n%s", cmd, usage)
}
