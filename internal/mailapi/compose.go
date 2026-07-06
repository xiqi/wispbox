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
	SizeBytes   int64
	Data        []byte
	Open        func() (io.ReadCloser, error)
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
	var buf bytes.Buffer
	if err := WriteMIME(&buf, o); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteMIME renders the message as RFC 5322 bytes to w. BCC recipients appear
// in the envelope only, never in headers.
func WriteMIME(w io.Writer, o *Outgoing) error {
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

	mw, err := mail.CreateWriter(w, h)
	if err != nil {
		return err
	}

	// A message with an HTML body is multipart/alternative: a plain-text part
	// first (for clients that can't or won't render HTML, and for better
	// deliverability) then the HTML part. Plain-text-only messages keep the
	// simpler single text/plain body.
	if o.HTMLBody != "" {
		iw, err := mw.CreateInline()
		if err != nil {
			return err
		}
		var th mail.InlineHeader
		th.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		tw, err := iw.CreatePart(th)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(tw, o.TextBody); err != nil {
			return err
		}
		tw.Close()

		var hh mail.InlineHeader
		hh.SetContentType("text/html", map[string]string{"charset": "utf-8"})
		hw, err := iw.CreatePart(hh)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(hw, o.HTMLBody); err != nil {
			return err
		}
		hw.Close()
		iw.Close()
	} else {
		var th mail.InlineHeader
		th.SetContentType("text/plain", map[string]string{"charset": "utf-8"})
		tw, err := mw.CreateSingleInline(th)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(tw, o.TextBody); err != nil {
			return err
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
			return err
		}
		src, err := att.open()
		if err != nil {
			aw.Close()
			return err
		}
		_, copyErr := io.Copy(aw, src)
		closeErr := src.Close()
		if copyErr != nil {
			aw.Close()
			return copyErr
		}
		if closeErr != nil {
			aw.Close()
			return closeErr
		}
		aw.Close()
	}
	return mw.Close()
}

func (a OutgoingAttachment) open() (io.ReadCloser, error) {
	if a.Open != nil {
		return a.Open()
	}
	return io.NopCloser(bytes.NewReader(a.Data)), nil
}

func toAddressList(addrs []string) []*mail.Address {
	out := make([]*mail.Address, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, &mail.Address{Address: a})
	}
	return out
}
