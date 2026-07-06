package admin

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// sendmailInject hands a message to the local Postfix via the sendmail
// binary. Only used on production servers for admin test emails; the message
// enters the normal queue and follows the active delivery policy.
func sendmailInject(ctx context.Context, path, from, to, subject, body string) error {
	if path == "" {
		path = "/usr/sbin/sendmail"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	msg := strings.Join([]string{
		"From: wispbox <" + from + ">",
		"To: <" + to + ">",
		"Subject: " + subject,
		"Date: " + time.Now().Format(time.RFC1123Z),
		"X-Mailer: wispbox",
		"",
		body,
	}, "\r\n")

	// "--" stops option parsing so a recipient that begins with "-" cannot be
	// interpreted as a sendmail flag. (from and to are already validated as
	// email addresses upstream.)
	cmd := exec.CommandContext(ctx, path, "-i", "-f", from, "--", to)
	cmd.Stdin = strings.NewReader(msg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("local mail injection failed: %s", detail)
	}
	return nil
}
