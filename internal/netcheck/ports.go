package netcheck

import (
	"context"
	"net"
	"strconv"
	"time"
)

func LoopbackTCPOpen(ctx context.Context, port int) bool {
	for _, host := range []string{"127.0.0.1", "::1"} {
		if TCPOpen(ctx, host, port, 700*time.Millisecond) {
			return true
		}
	}
	return false
}

func OutboundSMTP25Open(ctx context.Context) bool {
	return TCPOpen(ctx, "gmail-smtp-in.l.google.com", 25, 3*time.Second)
}

func TCPOpen(ctx context.Context, host string, port int, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
