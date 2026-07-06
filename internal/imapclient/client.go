// Package imapclient abstracts mailbox access for the Webmail API.
//
// Two adapters implement Client:
//
//   - Mock: an in-memory mailbox seeded with demo data (development, tests).
//   - Dovecot: a real IMAP client speaking to Dovecot on loopback.
//
// The Webmail API never loads whole mailboxes: List fetches headers for one
// page, Get fetches one message body, Attachment fetches one part.
package imapclient

import (
	"context"
	"time"

	"github.com/xiqi/wispbox/internal/auth"
)

// Well-known folder roles, mapped from IMAP SPECIAL-USE or name conventions.
const (
	RoleInbox  = "inbox"
	RoleSent   = "sent"
	RoleDrafts = "drafts"
	RoleTrash  = "trash"
	RoleJunk   = "junk"
	RoleCustom = "custom"
)

type Folder struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Total  uint32 `json:"total"`
	Unseen uint32 `json:"unseen"`
}

type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Header is the lightweight per-message data shown in list views.
type Header struct {
	UID            uint32    `json:"uid"`
	Folder         string    `json:"folder"`
	From           []Address `json:"from"`
	To             []Address `json:"to"`
	Subject        string    `json:"subject"`
	Date           time.Time `json:"date"`
	Seen           bool      `json:"seen"`
	Answered       bool      `json:"answered"`
	Flagged        bool      `json:"flagged"`
	HasAttachments bool      `json:"has_attachments"`
	Size           int64     `json:"size"`
}

// AttachmentMeta describes one attachment without its content.
type AttachmentMeta struct {
	Index     int    `json:"index"`
	Filename  string `json:"filename"`
	MIMEType  string `json:"mime_type"`
	Size      int64  `json:"size"`
	ContentID string `json:"content_id,omitempty"`
}

// Message is the full detail view: bodies and attachment metadata,
// but attachment content only on demand via Attachment().
type Message struct {
	Header
	CC          []Address        `json:"cc"`
	ReplyTo     []Address        `json:"reply_to"`
	MessageID   string           `json:"message_id"`
	InReplyTo   string           `json:"in_reply_to"`
	TextBody    string           `json:"text_body"`
	HTMLBody    string           `json:"html_body"` // raw; sanitized by the API layer
	Attachments []AttachmentMeta `json:"attachments"`
}

// AttachmentContent is one downloaded part.
type AttachmentContent struct {
	Filename string
	MIMEType string
	Data     []byte
}

// Page is one window of a folder listing, newest first.
type Page struct {
	Messages   []Header `json:"messages"`
	Total      uint32   `json:"total"`
	NextCursor string   `json:"next_cursor,omitempty"`
}

// Client is the mailbox access interface used by the Webmail API.
type Client interface {
	Folders(ctx context.Context, creds auth.Credentials) ([]Folder, error)
	List(ctx context.Context, creds auth.Credentials, folder, cursor string, limit int) (*Page, error)
	Search(ctx context.Context, creds auth.Credentials, folder, query string, limit int) (*Page, error)
	Get(ctx context.Context, creds auth.Credentials, folder string, uid uint32) (*Message, error)
	Attachment(ctx context.Context, creds auth.Credentials, folder string, uid uint32, index int) (*AttachmentContent, error)
	Move(ctx context.Context, creds auth.Credentials, folder string, uid uint32, dest string) error
	Delete(ctx context.Context, creds auth.Credentials, folder string, uid uint32) error
	SetSeen(ctx context.Context, creds auth.Credentials, folder string, uid uint32, seen bool) error
	Append(ctx context.Context, creds auth.Credentials, folder string, raw []byte, seen bool) error
}

// clampLimit applies the shared page-size policy: default 50, hard cap 100.
func clampLimit(n int) int {
	if n <= 0 || n > 100 {
		return 50
	}
	return n
}

// RoleForFolderName guesses a role from a folder name (fallback when the
// server does not advertise SPECIAL-USE).
func RoleForFolderName(name string) string {
	switch name {
	case "INBOX", "Inbox", "inbox":
		return RoleInbox
	case "Sent", "Sent Messages", "Sent Items":
		return RoleSent
	case "Drafts":
		return RoleDrafts
	case "Trash", "Deleted Messages":
		return RoleTrash
	case "Junk", "Spam":
		return RoleJunk
	}
	return RoleCustom
}
