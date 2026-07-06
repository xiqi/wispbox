package security

import (
	"strings"
	"testing"
)

func TestCheckOutgoingAttachment(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		size     int64
		wantErr  string
	}{
		{"pdf ok", "report.pdf", 1024, ""},
		{"image ok", "photo.jpg", 5 << 20, ""},
		{"no extension ok", "README", 10, ""},
		{"sh allowed for developers", "build.sh", 512, ""},
		{"exe rejected", "setup.exe", 1024, "cannot be sent"},
		{"uppercase extension rejected", "SETUP.EXE", 1024, "cannot be sent"},
		{"js rejected", "payload.js", 64, "cannot be sent"},
		{"bat rejected", "run.bat", 64, "cannot be sent"},
		{"hta rejected", "page.hta", 64, "cannot be sent"},
		{"disguised double extension rejected", "invoice.pdf.scr", 64, "cannot be sent"},
		{"at size limit ok", "big.pdf", MaxAttachmentSize, ""},
		{"over size limit", "huge.pdf", MaxAttachmentSize + 1, "exceeds the 25 MB limit"},
		{"empty attachment", "empty.pdf", 0, "is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckOutgoingAttachment(tt.filename, tt.size)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("CheckOutgoingAttachment(%q, %d) = %v, want nil", tt.filename, tt.size, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("CheckOutgoingAttachment(%q, %d) = nil, want error containing %q", tt.filename, tt.size, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestSafeDownloadType(t *testing.T) {
	htmlHead := []byte("<html><body><script>document.cookie</script></body></html>")
	pdfHead := []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	textHead := []byte("just some plain text content")
	const octet = "application/octet-stream"

	tests := []struct {
		name     string
		declared string
		head     []byte
		want     string
	}{
		{"declared html downgraded", "text/html", textHead, octet},
		{"declared html with params downgraded", "TEXT/HTML; charset=utf-8", textHead, octet},
		{"declared svg downgraded", "image/svg+xml", textHead, octet},
		{"declared xml downgraded", "application/xml", textHead, octet},
		{"declared xhtml downgraded", "application/xhtml+xml", textHead, octet},
		{"image claim sniffing as html downgraded", "image/png", htmlHead, octet},
		{"pdf claim sniffing as html downgraded", "application/pdf", htmlHead, octet},
		{"pdf served as declared", "application/pdf", pdfHead, "application/pdf"},
		{"plain text keeps base type", "text/plain; charset=utf-8", textHead, "text/plain"},
		{"declared case and space normalized", "  Application/PDF ; name=x", pdfHead, "application/pdf"},
		{"empty declared falls back", "", textHead, octet},
		{"application/unknown falls back", "application/unknown", textHead, octet},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SafeDownloadType(tt.declared, tt.head); got != tt.want {
				t.Errorf("SafeDownloadType(%q) = %q, want %q", tt.declared, got, tt.want)
			}
		})
	}
}
