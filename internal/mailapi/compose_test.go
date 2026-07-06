package mailapi

import (
	"io"
	"strings"
	"testing"

	"github.com/xiqi/wispbox/internal/imapclient"
)

// TestBuildMIMEMultipartAlternative covers the rich-text send path: an HTML
// body produces a multipart/alternative message with both a plain-text
// fallback and the HTML part, and both round-trip through the parser.
func TestBuildMIMEMultipartAlternative(t *testing.T) {
	raw, err := BuildMIME(&Outgoing{
		From: "me@example.com", To: []string{"you@example.com"},
		Subject: "hi", TextBody: "plain fallback",
		HTMLBody: "<p>Hello <strong>world</strong></p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "multipart/alternative") {
		t.Error("expected multipart/alternative container")
	}
	if !strings.Contains(s, "text/plain") || !strings.Contains(s, "text/html") {
		t.Error("expected both text/plain and text/html parts")
	}
	msg, err := imapclient.ParseMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.TextBody, "plain fallback") {
		t.Errorf("text fallback not parsed: %q", msg.TextBody)
	}
	if !strings.Contains(msg.HTMLBody, "<strong>world</strong>") {
		t.Errorf("html body not parsed: %q", msg.HTMLBody)
	}
}

// TestBuildMIMEHTMLWithAttachment covers rich body + attachment: the
// alternative group and the attachment must coexist.
func TestBuildMIMEHTMLWithAttachment(t *testing.T) {
	raw, err := BuildMIME(&Outgoing{
		From: "me@example.com", To: []string{"you@example.com"},
		Subject: "hi", TextBody: "text", HTMLBody: "<b>rich</b>",
		Atts: []OutgoingAttachment{{Filename: "a.txt", ContentType: "text/plain", Data: []byte("hello")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := imapclient.ParseMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.HTMLBody, "rich") {
		t.Errorf("html body lost when attachment present: %q", msg.HTMLBody)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Filename != "a.txt" {
		t.Errorf("attachment lost: %+v", msg.Attachments)
	}
}

func TestBuildMIMEStreamsAttachmentFromOpener(t *testing.T) {
	opened := 0
	raw, err := BuildMIME(&Outgoing{
		From: "me@example.com", To: []string{"you@example.com"},
		Subject: "hi", TextBody: "text",
		Atts: []OutgoingAttachment{{
			Filename:    "streamed.txt",
			ContentType: "text/plain",
			SizeBytes:   int64(len("streamed body")),
			Open: func() (io.ReadCloser, error) {
				opened++
				return io.NopCloser(strings.NewReader("streamed body")), nil
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if opened != 1 {
		t.Fatalf("attachment opener called %d times, want 1", opened)
	}
	msg, err := imapclient.ParseMessage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Attachments) != 1 || msg.Attachments[0].Filename != "streamed.txt" {
		t.Errorf("attachment lost: %+v", msg.Attachments)
	}
}

func TestHTMLToTextFallback(t *testing.T) {
	got := htmlToText("<p>Hi <strong>there</strong></p><ul><li>one</li><li>two</li></ul>")
	for _, want := range []string{"Hi there", "• one", "• two"} {
		if !strings.Contains(got, want) {
			t.Errorf("htmlToText missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "<") {
		t.Errorf("htmlToText left tags: %q", got)
	}
}
