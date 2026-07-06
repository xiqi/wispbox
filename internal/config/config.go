// Package config loads and validates wispboxd runtime configuration.
//
// Configuration is a flat "key = value" file (default /etc/wispbox/wispbox.conf)
// plus a small set of command line flags. Development mode swaps privileged
// ports and host-affecting adapters for safe local equivalents.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Mode selects between production adapters and development mocks.
type Mode string

const (
	ModeProduction  Mode = "production"
	ModeDevelopment Mode = "development"
)

// Config is the resolved runtime configuration for wispboxd/wispboxctl.
type Config struct {
	Mode Mode

	// Directories.
	ConfigDir    string // /etc/wispbox
	GeneratedDir string // /etc/wispbox/generated
	DataDir      string // /var/lib/wispbox
	CertDir      string // /var/lib/wispbox/certs
	MailDir      string // /var/lib/wispbox/mail
	DKIMDir      string // /var/lib/wispbox/dkim
	LogDir       string // /var/log/wispbox
	DBPath       string // /var/lib/wispbox/wispbox.db
	SecretPath   string // /var/lib/wispbox/secret.key

	// Listeners.
	HTTPAddr  string // ":80" production, ":8080" development
	HTTPSAddr string // ":443" production, ":8443" development

	// ACME.
	ACMEDirectoryURL string // empty -> Let's Encrypt production (or staging in dev)
	ACMEEmail        string

	// Development conveniences.
	DevSeed bool // seed the dev database with demo data
}

const (
	LetsEncryptProduction = "https://acme-v02.api.letsencrypt.org/directory"
	LetsEncryptStaging    = "https://acme-staging-v02.api.letsencrypt.org/directory"
)

// ProductionDefaults returns the standard production layout.
func ProductionDefaults() *Config {
	c := &Config{
		Mode:             ModeProduction,
		ConfigDir:        "/etc/wispbox",
		DataDir:          "/var/lib/wispbox",
		LogDir:           "/var/log/wispbox",
		HTTPAddr:         ":80",
		HTTPSAddr:        ":443",
		ACMEDirectoryURL: LetsEncryptProduction,
	}
	c.applyDerived()
	return c
}

// DevelopmentDefaults returns a layout rooted in dir (e.g. ./devdata) that
// never touches system paths or privileged ports.
func DevelopmentDefaults(dir string) *Config {
	c := &Config{
		Mode:             ModeDevelopment,
		ConfigDir:        filepath.Join(dir, "etc"),
		DataDir:          filepath.Join(dir, "data"),
		LogDir:           filepath.Join(dir, "log"),
		HTTPAddr:         ":8080",
		HTTPSAddr:        ":8443",
		ACMEDirectoryURL: LetsEncryptStaging,
	}
	c.applyDerived()
	return c
}

func (c *Config) applyDerived() {
	if c.GeneratedDir == "" {
		c.GeneratedDir = filepath.Join(c.ConfigDir, "generated")
	}
	if c.CertDir == "" {
		c.CertDir = filepath.Join(c.DataDir, "certs")
	}
	if c.MailDir == "" {
		c.MailDir = filepath.Join(c.DataDir, "mail")
	}
	if c.DKIMDir == "" {
		c.DKIMDir = filepath.Join(c.DataDir, "dkim")
	}
	if c.DBPath == "" {
		c.DBPath = filepath.Join(c.DataDir, "wispbox.db")
	}
	if c.SecretPath == "" {
		c.SecretPath = filepath.Join(c.DataDir, "secret.key")
	}
}

// IsDev reports whether development adapters should be used.
func (c *Config) IsDev() bool { return c.Mode == ModeDevelopment }

// Load reads a key=value config file over the given defaults.
// A missing file is not an error: defaults are returned unchanged.
func Load(path string, defaults *Config) (*Config, error) {
	c := *defaults
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &c, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		key, val, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("%s:%d: expected key = value", path, line)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "mode":
			switch Mode(val) {
			case ModeProduction, ModeDevelopment:
				c.Mode = Mode(val)
			default:
				return nil, fmt.Errorf("%s:%d: invalid mode %q", path, line, val)
			}
		case "data_dir":
			c.DataDir = val
			c.CertDir, c.MailDir, c.DKIMDir, c.DBPath, c.SecretPath = "", "", "", "", ""
		case "config_dir":
			c.ConfigDir = val
			c.GeneratedDir = ""
		case "log_dir":
			c.LogDir = val
		case "http_addr":
			c.HTTPAddr = val
		case "https_addr":
			c.HTTPSAddr = val
		case "acme_directory_url":
			c.ACMEDirectoryURL = val
		case "acme_email":
			c.ACMEEmail = val
		default:
			return nil, fmt.Errorf("%s:%d: unknown key %q", path, line, key)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	c.applyDerived()
	return &c, nil
}

// EnsureDirs creates the writable directory tree. It never touches paths
// outside the configured directories.
func (c *Config) EnsureDirs() error {
	dirs := []string{
		c.ConfigDir, c.GeneratedDir,
		filepath.Join(c.GeneratedDir, "postfix"),
		filepath.Join(c.GeneratedDir, "dovecot"),
		filepath.Join(c.GeneratedDir, "opendkim"),
		c.DataDir, c.CertDir, c.MailDir, c.DKIMDir, c.LogDir,
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}
	return nil
}
