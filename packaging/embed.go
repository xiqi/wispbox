// Package packaging embeds the Postfix, Dovecot, and OpenDKIM configuration
// templates so wispboxd can render them without any files on disk.
package packaging

import "embed"

//go:embed postfix/templates dovecot/templates opendkim/templates
var Templates embed.FS
