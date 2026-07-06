// Package services abstracts every interaction with host services
// (systemd, postfix, dovecot, journald). Production adapters shell out;
// the mock adapter records events so development and tests never touch
// the host.
package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/xiqi/wispbox/internal/db"
)

// Manager reloads/restarts services and reports their state.
type Manager interface {
	Reload(ctx context.Context, service string) error
	Restart(ctx context.Context, service string) error
	IsActive(ctx context.Context, service string) (bool, error)
}

// ---- systemd (production) ----

// SystemdManager drives services through systemctl. Only ever used on the
// user's server, never in development mode.
type SystemdManager struct {
	store *db.Store
}

func NewSystemdManager(store *db.Store) *SystemdManager { return &SystemdManager{store: store} }

var allowedServices = map[string]bool{"postfix": true, "dovecot": true, "opendkim": true, "wispboxd": true}

// systemctlCommand runs systemctl directly as root, or through the narrow
// sudoers allowlist the installer creates (/etc/sudoers.d/wispbox) when
// running as the wispbox user.
func systemctlCommand(ctx context.Context, args ...string) *exec.Cmd {
	if os.Geteuid() == 0 {
		return exec.CommandContext(ctx, "systemctl", args...)
	}
	return exec.CommandContext(ctx, "sudo", append([]string{"-n", "systemctl"}, args...)...)
}

func (m *SystemdManager) run(ctx context.Context, verb, service string) error {
	if !allowedServices[service] {
		return fmt.Errorf("refusing to %s unknown service %q", verb, service)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := systemctlCommand(ctx, verb, service).CombinedOutput()
	status := "ok"
	msg := ""
	if err != nil {
		status = "error"
		msg = strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
	}
	if m.store != nil {
		_ = m.store.AppendServiceEvent(ctx, db.ServiceEvent{Service: service, EventType: verb, Status: status, Message: msg})
	}
	if err != nil {
		return fmt.Errorf("systemctl %s %s: %s", verb, service, msg)
	}
	return nil
}

func (m *SystemdManager) Reload(ctx context.Context, service string) error {
	return m.run(ctx, "reload-or-restart", service)
}

func (m *SystemdManager) Restart(ctx context.Context, service string) error {
	return m.run(ctx, "restart", service)
}

func (m *SystemdManager) IsActive(ctx context.Context, service string) (bool, error) {
	if !allowedServices[service] {
		return false, fmt.Errorf("unknown service %q", service)
	}
	out, err := exec.CommandContext(ctx, "systemctl", "is-active", service).Output()
	state := strings.TrimSpace(string(out))
	if state == "active" {
		return true, nil
	}
	_ = err // non-zero exit just means inactive/failed
	return false, nil
}

// ---- mock (development and tests) ----

// MockManager records service actions without touching the host.
type MockManager struct {
	store *db.Store

	mu      sync.Mutex
	Actions []string        // e.g. "reload postfix"
	Active  map[string]bool // service -> simulated active state
	FailOn  map[string]bool // service -> force errors (tests)
}

func NewMockManager(store *db.Store) *MockManager {
	return &MockManager{
		store:  store,
		Active: map[string]bool{"postfix": true, "dovecot": true, "opendkim": true, "wispboxd": true},
		FailOn: map[string]bool{},
	}
}

func (m *MockManager) record(ctx context.Context, verb, service string) error {
	m.mu.Lock()
	m.Actions = append(m.Actions, verb+" "+service)
	fail := m.FailOn[service]
	m.mu.Unlock()
	status, msg := "ok", "(dev mode: no real service touched)"
	if fail {
		status, msg = "error", "simulated failure"
	}
	if m.store != nil {
		_ = m.store.AppendServiceEvent(ctx, db.ServiceEvent{Service: service, EventType: verb, Status: status, Message: msg})
	}
	if fail {
		return fmt.Errorf("mock %s %s: simulated failure", verb, service)
	}
	return nil
}

func (m *MockManager) Reload(ctx context.Context, service string) error {
	return m.record(ctx, "reload", service)
}

func (m *MockManager) Restart(ctx context.Context, service string) error {
	return m.record(ctx, "restart", service)
}

func (m *MockManager) IsActive(_ context.Context, service string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Active[service], nil
}
