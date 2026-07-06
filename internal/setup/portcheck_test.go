package setup

import (
	"context"
	"net"
	"strconv"
	"testing"

	"github.com/xiqi/wispbox/internal/config"
	"github.com/xiqi/wispbox/internal/netcheck"
)

func TestLoopbackTCPOpen(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	_, portText, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("Atoi: %v", err)
	}
	if !netcheck.LoopbackTCPOpen(context.Background(), port) {
		t.Fatalf("LoopbackTCPOpen(%d) = false, want true", port)
	}
}

func TestSetupPortChecksDevMode(t *testing.T) {
	checks, outbound := setupPortChecks(context.Background(), config.DevelopmentDefaults(t.TempDir()))
	if outbound != nil {
		t.Fatalf("development outbound check = %v, want nil", *outbound)
	}
	if len(checks) != 1 || checks[0].Name != "Ports" || !checks[0].OK || checks[0].Required {
		t.Fatalf("development checks = %+v, want one non-required ok Ports check", checks)
	}
}
