package security

import (
	"strings"
	"testing"
)

func TestSanitizeEmailHTML(t *testing.T) {
	tests := []struct {
		name         string
		html         string
		allowRemote  bool
		wantContains []string
		wantAbsent   []string
		wantRemote   bool
	}{
		{
			name:       "script tags stripped",
			html:       `<script>alert(1)</script><p>safe text</p>`,
			wantAbsent: []string{"<script", "alert(1)"},
			wantContains: []string{
				"<p>safe text</p>",
			},
		},
		{
			name:         "event handlers stripped",
			html:         `<p onclick="steal()" onmouseover="steal()">hello</p>`,
			wantContains: []string{"hello"},
			wantAbsent:   []string{"onclick", "onmouseover", "steal"},
		},
		{
			name:         "iframe and form stripped",
			html:         `<iframe src="https://evil.example"></iframe><form action="https://evil.example"><input name="pw"></form><p>body</p>`,
			wantContains: []string{"<p>body</p>"},
			wantAbsent:   []string{"<iframe", "<form", "<input"},
		},
		{
			name:         "javascript href stripped",
			html:         `<a href="javascript:alert(1)">click</a>`,
			wantContains: []string{"click"},
			wantAbsent:   []string{"javascript:"},
		},
		{
			name:         "remote img removed by default",
			html:         `<img src="https://tracker.example/pixel.gif" alt="p"><p>hi</p>`,
			wantContains: []string{`alt="p"`, "<p>hi</p>"},
			wantAbsent:   []string{"tracker.example", "src="},
			wantRemote:   true,
		},
		{
			name:         "data image kept when blocked",
			html:         `<img src="data:image/png;base64,iVBORw0KGgo=" alt="inline">`,
			wantContains: []string{`src="data:image/png;base64,iVBORw0KGgo="`},
			wantRemote:   false,
		},
		{
			name:         "attachment api image kept when blocked",
			html:         `<img src="/api/mail/attachments/42" alt="att">`,
			wantContains: []string{`src="/api/mail/attachments/42"`},
			wantRemote:   false,
		},
		{
			name:         "non-image data uri stripped",
			html:         `<img src="data:text/html;base64,PHNjcmlwdD4=" alt="sneaky">`,
			wantContains: []string{`alt="sneaky"`},
			wantAbsent:   []string{"data:text/html"},
			wantRemote:   false,
		},
		{
			name:         "remote img kept when allowed",
			html:         `<img src="https://tracker.example/pixel.gif" alt="p">`,
			allowRemote:  true,
			wantContains: []string{`src="https://tracker.example/pixel.gif"`},
			wantRemote:   true,
		},
		{
			name:         "css url neutralized when blocked",
			html:         `<div style="background: url(https://evil.example/x.png); color: red">x</div>`,
			wantContains: []string{"color: red"},
			wantAbsent:   []string{"evil.example"},
			wantRemote:   false,
		},
		{
			name:       "css url with whitespace neutralized when blocked",
			html:       `<div style="background: URL ('https://evil.example/x.png')">x</div>`,
			wantAbsent: []string{"evil.example"},
			wantRemote: false,
		},
		{
			name:         "styles survive on safe content",
			html:         `<p style="color: blue; font-size: 14px">styled</p>`,
			wantContains: []string{"color: blue", "font-size: 14px", "styled"},
			wantRemote:   false,
		},
		{
			name:         "links gain nofollow",
			html:         `<a href="https://example.com">link</a>`,
			wantContains: []string{`href="https://example.com"`, `rel="nofollow"`},
			wantRemote:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clean, hadRemote := SanitizeEmailHTML(tt.html, tt.allowRemote)
			if hadRemote != tt.wantRemote {
				t.Errorf("hadRemote = %v, want %v", hadRemote, tt.wantRemote)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(clean, want) {
					t.Errorf("output missing %q\noutput: %s", want, clean)
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(clean, absent) {
					t.Errorf("output still contains %q\noutput: %s", absent, clean)
				}
			}
		})
	}
}
