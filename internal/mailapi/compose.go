package mailapi

import (
	"bytes"
	"fmt"
	"io"
	"time"

	"github.com/emersion/go-message/mail"
)

// OutgoingAttachment is one file attached to an outgoing message.
type OutgoingAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// Outgoing is a fully specified message to build and send.
type Outgoing struct {
	From      string
	To        []string
	CC        []string
	BCC       []string
	Subject   string
	TextBody  string
	HTMLBody  string // when set, the message is multipart/alternative
	InReplyTo string
	Atts      []OutgoingAttachment
}

// AllRecipients returns To+CC+BCC for the SMTP envelope.
func (o *Outgoing) AllRecipients() []string {
	out := make([]string, 0, len(o.To)+len(o.CC)+len(o.BCC))
	out = append(out, o.To...)
	out = append(out, o.CC...)
	out = append(out, o.BCC...)
	return out
}

// BuildMIME renders the message as RFC 5322 bytes. BCC recipients appear in
// the envelope only, never in headers.
func BuildMIME(o *Outgoing) ([]byte, error) {
	var h mail.Header
	h.SetDate(time.Now())
	h.SetAddressList("From", []*mail.Address{{Address: o.From}})
	h.SetAddressList("To", toAddressList(o.To))
	if len(o.CC) > 0 {
		h.SetAddressList("Cc", toAddressList(o.CC))
	}
	h.SetSubject(o.Subject)
	h.Set("Message-Id", fmt.Sprintf("<%d.%s>", time.Now().UnixNano(), o.From))
	if o.InReplyTo != "" {
		h.Set("In-Reply-To", "<"+o.InReplyTo+">")
		h.Set("References", "<"+o.InReplyTo+">")
	}
	h.Set("X-Mailer", "wispbox")

	var buf bytes.Buffer
	mw, err := mail.CreateWriter(&buf, h)
	if err != nil {
		return nil, err
	}

	// A message with an HTML body is multipart/alternative: a plain-text part
	// first (for clients that can't or won't render HTML, and for better
	// deliverability) then the HTML part. Plain-text-only messages keep the
	// simpler single text/plain body.
	if o.HTMLBody != "" {
		iw, err := mw.CreateInline()
		if err != nil {
			return nil, err
		}
		var th mail.InlineHeader
		th.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		tw, err := iw.CreatePart(th)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(tw, o.TextBody); err != nil {
			return nil, err
		}
		tw.Close()

		var hh mail.InlineHeader
		hh.SetContentType("text/html", map[string]string{"charset": "utf-8"})
		hw, err := iw.CreatePart(hh)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(hw, o.HTMLBody); err != nil {
			return nil, err
		}
		hw.Close()
		iw.Close()
	} else {
		var th mail.InlineHeader
		th.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		tw, err := mw.CreateSingleInline(th)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(tw, o.TextBody); err != nil {
			return nil, err
		}
		tw.Close()
	}

	for _, att := range o.Atts {
		var ah mail.AttachmentHeader
		ct := att.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		ah.SetContentType(ct, nil)
		ah.SetFilename(att.Filename)
		aw, err := mw.CreateAttachment(ah)
		if err != nil {
			return nil, err
		}
		if _, err := aw.Write(att.Data); err != nil {
			return nil, err
		}
		aw.Close()
	}
	mw.Close()
	return buf.Bytes(), nil
}

func toAddressList(addrs []string) []*mail.Address {
	out := make([]*mail.Address, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, &mail.Address{Address: a})
	}
	return out
}
