package setup

import (
	"context"

	"github.com/xiqi/wispbox/internal/config"
	"github.com/xiqi/wispbox/internal/netcheck"
)

type setupCheck struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
	Required bool   `json:"required"`
}

func setupPortChecks(ctx context.Context, cfg *config.Config) ([]setupCheck, *bool) {
	if cfg.IsDev() {
		return []setupCheck{{
			Name:     "Ports",
			OK:       true,
			Detail:   "development mode uses high ports and mock mail services",
			Required: false,
		}}, nil
	}

	checks := []setupCheck{
		localPortCheck(ctx, "HTTP port 80", 80, "Required for Let's Encrypt certificate issuance."),
		localPortCheck(ctx, "HTTPS port 443", 443, "Required for admin and webmail."),
		localPortCheck(ctx, "SMTP port 25", 25, "Required to receive mail from other servers."),
		localPortCheck(ctx, "Submission port 587", 587, "Required for users to send mail from mail clients."),
		localPortCheck(ctx, "IMAP port 993", 993, "Required for users to read mail from mail clients."),
	}

	outboundOK := netcheck.OutboundSMTP25Open(ctx)
	detail := "Outbound port 25 is open; direct sending can work if DNS, PTR, and reputation are clean."
	if !outboundOK {
		detail = "Outbound port 25 looks blocked. Choose relay mode for reliable sending."
	}
	checks = append(checks, setupCheck{
		Name:     "Outbound SMTP 25",
		OK:       outboundOK,
		Detail:   detail,
		Required: false,
	})
	return checks, &outboundOK
}

func localPortCheck(ctx context.Context, name string, port int, purpose string) setupCheck {
	ok := netcheck.LoopbackTCPOpen(ctx, port)
	detail := purpose + " Listening locally."
	if !ok {
		detail = purpose + " Nothing is listening locally; check the service and firewall."
	}
	return setupCheck{Name: name, OK: ok, Detail: detail, Required: true}
}
