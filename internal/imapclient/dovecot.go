package imapclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2"
	goimap "github.com/emersion/go-imap/v2/imapclient"

	"github.com/xiqi/wispbox/internal/auth"
	"github.com/xiqi/wispbox/internal/security"
)

// Dovecot talks to the local Dovecot IMAP listener (loopback, plaintext —
// loopback connections are "secured" in Dovecot terms). It opens one
// connection per operation: boring, stateless, and well within the load a
// small team generates.
type Dovecot struct {
	Addr    string // e.g. 127.0.0.1:143
	Timeout time.Duration
}

func NewDovecot(addr string) *Dovecot {
	return &Dovecot{Addr: addr, Timeout: 30 * time.Second}
}

func (d *Dovecot) dial(creds auth.Credentials) (*goimap.Client, error) {
	timeout := d.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	conn, err := net.DialTimeout("tcp", d.Addr, timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to mail server: %w", err)
	}
	// Bound every operation on this connection so a stuck Dovecot can never
	// hang a webmail request forever.
	_ = conn.SetDeadline(time.Now().Add(timeout))
	c := goimap.New(conn, nil)
	if err := c.Login(creds.Email, creds.Password).Wait(); err != nil {
		c.Close()
		return nil, fmt.Errorf("mail server rejected the session; please sign in again")
	}
	return c, nil
}

func (d *Dovecot) Folders(ctx context.Context, creds auth.Credentials) ([]Folder, error) {
	c, err := d.dial(creds)
	if err != nil {
		return nil, err
	}
	defer c.Logout()

	listCmd := c.List("", "*", &imap.ListOptions{
		ReturnStatus: &imap.StatusOptions{NumMessages: true, NumUnseen: true},
	})
	boxes, err := listCmd.Collect()
	if err != nil {
		return nil, fmt.Errorf("list folders: %w", err)
	}
	var out []Folder
	for _, b := range boxes {
		f := Folder{Name: b.Mailbox, Role: roleForAttrs(b.Attrs, b.Mailbox)}
		if b.Status != nil {
			if b.Status.NumMessages != nil {
				f.Total = *b.Status.NumMessages
			}
			if b.Status.NumUnseen != nil {
				f.Unseen = *b.Status.NumUnseen
			}
		}
		out = append(out, f)
	}
	return out, nil
}

func roleForAttrs(attrs []imap.MailboxAttr, name string) string {
	for _, a := range attrs {
		switch a {
		case imap.MailboxAttrSent:
			return RoleSent
		case imap.MailboxAttrDrafts:
			return RoleDrafts
		case imap.MailboxAttrTrash:
			return RoleTrash
		case imap.MailboxAttrJunk:
			return RoleJunk
		}
	}
	return RoleForFolderName(name)
}

func (d *Dovecot) List(ctx context.Context, creds auth.Credentials, folder, cursor string, limit int) (*Page, error) {
	limit = clampLimit(limit)
	offset := 0
	if cursor != "" {
		offset, _ = strconv.Atoi(cursor)
	}

	c, err := d.dial(creds)
	if err != nil {
		return nil, err
	}
	defer c.Logout()

	sel, err := c.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("open folder %s: %w", folder, err)
	}
	total := sel.NumMessages
	page := &Page{Total: total}
	if total == 0 || uint32(offset) >= total {
		return page, nil
	}

	// Newest messages have the highest sequence numbers. Window the range
	// [hi-limit+1, hi] where hi = total - offset.
	hi := total - uint32(offset)
	lo := uint32(1)
	if hi > uint32(limit) {
		lo = hi - uint32(limit) + 1
	}
	var seqSet imap.SeqSet
	seqSet.AddRange(lo, hi)

	msgs, err := c.Fetch(seqSet, &imap.FetchOptions{
		Envelope: true, Flags: true, RFC822Size: true, UID: true,
		BodyStructure: &imap.FetchItemBodyStructure{},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch headers: %w", err)
	}
	// Collect returns ascending sequence order; reverse for newest-first.
	for i := len(msgs) - 1; i >= 0; i-- {
		page.Messages = append(page.Messages, headerFromFetch(msgs[i], folder))
	}
	if uint32(offset+limit) < total {
		page.NextCursor = strconv.Itoa(offset + limit)
	}
	return page, nil
}

func (d *Dovecot) Search(ctx context.Context, creds auth.Credentials, folder, query string, limit int) (*Page, error) {
	limit = clampLimit(limit)
	c, err := d.dial(creds)
	if err != nil {
		return nil, err
	}
	defer c.Logout()

	if _, err := c.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return nil, fmt.Errorf("open folder %s: %w", folder, err)
	}
	// IMAP SEARCH TEXT: matches headers and body, executed server-side.
	data, err := c.UIDSearch(&imap.SearchCriteria{Text: []string{query}}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	uids := data.AllUIDs()
	page := &Page{Total: uint32(len(uids))}
	if len(uids) == 0 {
		return page, nil
	}
	// Newest results: highest UIDs last in the result; take the tail.
	if len(uids) > limit {
		uids = uids[len(uids)-limit:]
	}
	var uidSet imap.UIDSet
	for _, u := range uids {
		uidSet.AddNum(u)
	}
	msgs, err := c.Fetch(uidSet, &imap.FetchOptions{
		Envelope: true, Flags: true, RFC822Size: true, UID: true,
		BodyStructure: &imap.FetchItemBodyStructure{},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch search results: %w", err)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		page.Messages = append(page.Messages, headerFromFetch(msgs[i], folder))
	}
	return page, nil
}

func headerFromFetch(m *goimap.FetchMessageBuffer, folder string) Header {
	h := Header{
		UID:    uint32(m.UID),
		Folder: folder,
		Size:   m.RFC822Size,
	}
	applyFlags(&h, m.Flags)
	if env := m.Envelope; env != nil {
		h.Subject = env.Subject
		h.Date = env.Date
		h.From = convertIMAPAddrs(env.From)
		h.To = convertIMAPAddrs(env.To)
	}
	if m.BodyStructure != nil {
		m.BodyStructure.Walk(func(path []int, part imap.BodyStructure) bool {
			if sp, ok := part.(*imap.BodyStructureSinglePart); ok {
				if disp := sp.Disposition(); disp != nil && disp.Value == "attachment" {
					h.HasAttachments = true
				}
			}
			return true
		})
	}
	return h
}

func convertIMAPAddrs(addrs []imap.Address) []Address {
	var out []Address
	for _, a := range addrs {
		out = append(out, Address{Name: a.Name, Email: a.Addr()})
	}
	return out
}

// fetchRaw downloads the full raw message for uid.
func (d *Dovecot) fetchRaw(c *goimap.Client, uid uint32) ([]byte, []imap.Flag, error) {
	uidSet := imap.UIDSetNum(imap.UID(uid))
	meta, err := c.Fetch(uidSet, &imap.FetchOptions{
		UID: true, Flags: true, RFC822Size: true,
	}).Collect()
	if err != nil {
		return nil, nil, fmt.Errorf("fetch message metadata: %w", err)
	}
	if len(meta) == 0 {
		return nil, nil, fmt.Errorf("message not found")
	}
	flags := meta[0].Flags
	if meta[0].RFC822Size > security.MaxIncomingMessageSize {
		return nil, flags, fmt.Errorf("message is too large to open in webmail (%d MB limit)", security.MaxIncomingMessageSizeMB)
	}

	section := &imap.FetchItemBodySection{Peek: true}
	msgs, err := c.Fetch(uidSet, &imap.FetchOptions{
		UID: true, BodySection: []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return nil, nil, fmt.Errorf("fetch message: %w", err)
	}
	if len(msgs) == 0 {
		return nil, nil, fmt.Errorf("message not found")
	}
	raw := msgs[0].FindBodySection(section)
	if raw == nil {
		return nil, nil, fmt.Errorf("message body unavailable")
	}
	return raw, flags, nil
}

func (d *Dovecot) Get(ctx context.Context, creds auth.Credentials, folder string, uid uint32) (*Message, error) {
	c, err := d.dial(creds)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	if _, err := c.Select(folder, nil).Wait(); err != nil {
		return nil, fmt.Errorf("open folder %s: %w", folder, err)
	}
	raw, flags, err := d.fetchRaw(c, uid)
	if err != nil {
		return nil, err
	}
	msg, err := ParseMessage(raw)
	if err != nil {
		return nil, err
	}
	msg.UID = uid
	msg.Folder = folder
	applyFlags(&msg.Header, flags)
	return msg, nil
}

// applyFlags copies the IMAP flags wispbox tracks onto a Header.
func applyFlags(h *Header, flags []imap.Flag) {
	for _, f := range flags {
		switch f {
		case imap.FlagSeen:
			h.Seen = true
		case imap.FlagAnswered:
			h.Answered = true
		case imap.FlagFlagged:
			h.Flagged = true
		}
	}
}

func (d *Dovecot) Attachment(ctx context.Context, creds auth.Credentials, folder string, uid uint32, index int) (*AttachmentContent, error) {
	c, err := d.dial(creds)
	if err != nil {
		return nil, err
	}
	defer c.Logout()
	if _, err := c.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return nil, fmt.Errorf("open folder %s: %w", folder, err)
	}
	raw, _, err := d.fetchRaw(c, uid)
	if err != nil {
		return nil, err
	}
	return ExtractAttachment(raw, index)
}

func (d *Dovecot) Move(ctx context.Context, creds auth.Credentials, folder string, uid uint32, dest string) error {
	c, err := d.dial(creds)
	if err != nil {
		return err
	}
	defer c.Logout()
	if _, err := c.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("open folder %s: %w", folder, err)
	}
	uidSet := imap.UIDSetNum(imap.UID(uid))
	if _, err := c.Move(uidSet, dest).Wait(); err != nil {
		return fmt.Errorf("move message: %w", err)
	}
	return nil
}

func (d *Dovecot) Delete(ctx context.Context, creds auth.Credentials, folder string, uid uint32) error {
	c, err := d.dial(creds)
	if err != nil {
		return err
	}
	defer c.Logout()
	if _, err := c.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("open folder %s: %w", folder, err)
	}
	uidSet := imap.UIDSetNum(imap.UID(uid))
	err = c.Store(uidSet, &imap.StoreFlags{
		Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted},
	}, nil).Close()
	if err != nil {
		return fmt.Errorf("flag message deleted: %w", err)
	}
	// UID EXPUNGE removes only this message, even if another client has
	// flagged other messages \Deleted concurrently. Fall back to a plain
	// EXPUNGE if the server lacks UIDPLUS (Dovecot always has it).
	if err := c.UIDExpunge(uidSet).Close(); err != nil {
		if err := c.Expunge().Close(); err != nil {
			return fmt.Errorf("expunge: %w", err)
		}
	}
	return nil
}

func (d *Dovecot) SetSeen(ctx context.Context, creds auth.Credentials, folder string, uid uint32, seen bool) error {
	c, err := d.dial(creds)
	if err != nil {
		return err
	}
	defer c.Logout()
	if _, err := c.Select(folder, nil).Wait(); err != nil {
		return fmt.Errorf("open folder %s: %w", folder, err)
	}
	op := imap.StoreFlagsAdd
	if !seen {
		op = imap.StoreFlagsDel
	}
	uidSet := imap.UIDSetNum(imap.UID(uid))
	if err := c.Store(uidSet, &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close(); err != nil {
		return fmt.Errorf("update flags: %w", err)
	}
	return nil
}

func (d *Dovecot) Append(ctx context.Context, creds auth.Credentials, folder string, raw []byte, seen bool) error {
	return d.AppendReader(ctx, creds, folder, int64(len(raw)), bytes.NewReader(raw), seen)
}

func (d *Dovecot) AppendReader(ctx context.Context, creds auth.Credentials, folder string, size int64, raw io.Reader, seen bool) error {
	c, err := d.dial(creds)
	if err != nil {
		return err
	}
	defer c.Logout()
	opts := &imap.AppendOptions{}
	if seen {
		opts.Flags = []imap.Flag{imap.FlagSeen}
	}
	cmd := c.Append(folder, size, opts)
	if _, err := io.Copy(cmd, raw); err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	if err := cmd.Close(); err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	if _, err := cmd.Wait(); err != nil {
		return fmt.Errorf("append message: %w", err)
	}
	return nil
}
