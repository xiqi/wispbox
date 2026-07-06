package admin

import "testing"

func TestLooksLikeActionableLogFiltersOverviewNoise(t *testing.T) {
	tests := []struct {
		name    string
		service string
		message string
		want    bool
	}{
		{
			name:    "postfix delivery timeout is actionable",
			service: "postfix",
			message: "connect to aspmx.l.google.com[74.125.199.26]:25: Connection timed out",
			want:    true,
		},
		{
			name:    "deferred delivery is actionable",
			service: "postfix",
			message: "ABC123: to=<user@example.com>, relay=none, delay=10, status=deferred (connect timed out)",
			want:    true,
		},
		{
			name:    "oom is actionable",
			service: "wispboxd",
			message: "Failed with result 'oom-kill'",
			want:    true,
		},
		{
			name:    "certificate failure is actionable",
			service: "wispboxd",
			message: "certificate mail.example.com failed: DNS record points elsewhere",
			want:    true,
		},
		{
			name:    "tls unknown cert browser noise ignored",
			service: "wispboxd",
			message: "http: TLS handshake error from [2600:1f13::1]:51948: remote error: tls: unknown certificate",
			want:    false,
		},
		{
			name:    "external scanner disconnect ignored",
			service: "postfix",
			message: "SSL_accept error from cloud-scanner.example[45.33.91.56]: lost connection",
			want:    false,
		},
		{
			name:    "postfix symlink warning ignored",
			service: "postfix",
			message: "postfix/postlog: warning: symlink leaves directory: /etc/postfix/./main.cf",
			want:    false,
		},
		{
			name:    "normal dovecot restart signal ignored",
			service: "dovecot",
			message: "master: Warning: Killed with signal 15 (by pid=15522 uid=0 code=kill)",
			want:    false,
		},
		{
			name:    "normal systemd sigterm ignored",
			service: "dovecot",
			message: "Main process exited, code=killed, status=15/TERM",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeActionableLog(tt.service, tt.message); got != tt.want {
				t.Fatalf("looksLikeActionableLog() = %v, want %v", got, tt.want)
			}
		})
	}
}
