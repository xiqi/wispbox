package imapclient

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/emersion/go-message/mail"

	_ "github.com/emersion/go-message/charset" // register legacy charsets
	"github.com/xiqi/wispbox/internal/security"
)

// ParseMessage extracts bodies and attachment metadata from a raw RFC 5322
// message. Used by the Dovecot adapter (which fetches full messages on
// demand) and for appended drafts in the mock.
func ParseMessage(raw []byte) (*Message, error) {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil && mr == nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	msg := &Message{}
	h := mr.Header
	msg.Subject, _ = h.Subject()
	msg.Date, _ = h.Date()
	msg.MessageID, _ = h.MessageID()
	if refs, err := h.MsgIDList("In-Reply-To"); err == nil && len(refs) > 0 {
		msg.InReplyTo = refs[0]
	}
	msg.From = convertAddressList(h, "From")
	msg.To = convertAddressList(h, "To")
	msg.CC = convertAddressList(h, "Cc")
	msg.ReplyTo = convertAddressList(h, "Reply-To")
	msg.Size = int64(len(raw))

	attIndex := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Tolerate malformed sub-parts; show what we could parse.
			break
		}
		switch ph := part.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := ph.ContentType()
			body, _ := io.ReadAll(io.LimitReader(part.Body, security.MaxInlinePartSize+1))
			if int64(len(body)) > security.MaxInlinePartSize {
				body = body[:security.MaxInlinePartSize]
			}
			switch {
			case strings.HasPrefix(ct, "text/plain") && msg.TextBody == "":
				msg.TextBody = string(body)
			case strings.HasPrefix(ct, "text/html") && msg.HTMLBody == "":
				msg.HTMLBody = string(body)
			case strings.HasPrefix(ct, "image/"):
				// Inline image without attachment disposition: expose it as
				// an attachment so cid: references can be resolved.
				cid := strings.Trim(ph.Get("Content-Id"), "<>")
				msg.Attachments = append(msg.Attachments, AttachmentMeta{
					Index: attIndex, Filename: inlineImageName(ct, attIndex),
					MIMEType: ct, Size: int64(len(body)), ContentID: cid,
				})
				attIndex++
			}
		case *mail.AttachmentHeader:
			filename, _ := ph.Filename()
			if filename == "" {
				filename = fmt.Sprintf("attachment-%d", attIndex+1)
			}
			ct, _, _ := ph.ContentType()
			n, _ := io.Copy(io.Discard, part.Body)
			cid := strings.Trim(ph.Get("Content-Id"), "<>")
			msg.Attachments = append(msg.Attachments, AttachmentMeta{
				Index: attIndex, Filename: filename, MIMEType: ct, Size: n, ContentID: cid,
			})
			attIndex++
		}
	}
	msg.HasAttachments = len(msg.Attachments) > 0
	return msg, nil
}

// ExtractAttachment returns the decoded content of the attachment at index
// (matching the numbering produced by ParseMessage).
func ExtractAttachment(raw []byte, index int) (*AttachmentContent, error) {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil && mr == nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	attIndex := 0
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch ph := part.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := ph.ContentType()
			if strings.HasPrefix(ct, "image/") {
				if attIndex == index {
					data, err := io.ReadAll(part.Body)
					if err != nil {
						return nil, err
					}
					return &AttachmentContent{Filename: inlineImageName(ct, attIndex), MIMEType: ct, Data: data}, nil
				}
				attIndex++
			}
		case *mail.AttachmentHeader:
			if attIndex == index {
				filename, _ := ph.Filename()
				if filename == "" {
					filename = fmt.Sprintf("attachment-%d", attIndex+1)
				}
				ct, _, _ := ph.ContentType()
				data, err := io.ReadAll(part.Body)
				if err != nil {
					return nil, err
				}
				return &AttachmentContent{Filename: filename, MIMEType: ct, Data: data}, nil
			}
			attIndex++
		}
	}
	return nil, fmt.Errorf("attachment %d not found", index)
}

func convertAddressList(h mail.Header, field string) []Address {
	list, err := h.AddressList(field)
	if err != nil {
		return nil
	}
	out := make([]Address, 0, len(list))
	for _, a := range list {
		out = append(out, Address{Name: a.Name, Email: a.Address})
	}
	return out
}

func inlineImageName(contentType string, index int) string {
	ext := "img"
	if _, sub, ok := strings.Cut(contentType, "/"); ok {
		ext = sub
	}
	return fmt.Sprintf("inline-%d.%s", index+1, ext)
}
