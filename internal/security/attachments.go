package security

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

// Extensions that are executable or script-like on common desktop platforms.
// wispbox refuses to send them and never serves them with an active type.
var dangerousExtensions = map[string]bool{
	".exe": true, ".msi": true, ".bat": true, ".cmd": true, ".com": true,
	".scr": true, ".pif": true, ".cpl": true, ".jar": true, ".js": true,
	".jse": true, ".vbs": true, ".vbe": true, ".wsf": true, ".wsh": true,
	".ps1": true, ".psm1": true, ".msh": true, ".hta": true, ".apk": true,
	".app": true, ".dmg": true, ".sh": false, // .sh allowed: common between developers
}

const MaxAttachmentSize = 25 << 20 // 25 MiB per attachment

// CheckOutgoingAttachment validates an attachment before sending.
func CheckOutgoingAttachment(filename string, size int64) error {
	ext := strings.ToLower(filepath.Ext(filename))
	if dangerousExtensions[ext] {
		return fmt.Errorf("attachments of type %s cannot be sent", ext)
	}
	if size > MaxAttachmentSize {
		return fmt.Errorf("attachment %s exceeds the 25 MB limit", filename)
	}
	if size == 0 {
		return fmt.Errorf("attachment %s is empty", filename)
	}
	return nil
}

// SafeDownloadType decides the Content-Type used when serving an attachment.
// If the declared MIME type disagrees with content sniffing in a dangerous
// way (e.g. claims image but is HTML), fall back to octet-stream. Everything
// is served with Content-Disposition: attachment and nosniff by the handler.
func SafeDownloadType(declared string, head []byte) string {
	declared = strings.ToLower(strings.TrimSpace(strings.Split(declared, ";")[0]))
	sniffed := http.DetectContentType(head)
	activeTypes := []string{"text/html", "application/xhtml", "image/svg", "text/xml", "application/xml"}
	for _, t := range activeTypes {
		if strings.HasPrefix(declared, t) || strings.HasPrefix(sniffed, t) {
			return "application/octet-stream"
		}
	}
	if declared == "" || declared == "application/unknown" {
		return "application/octet-stream"
	}
	return declared
}
