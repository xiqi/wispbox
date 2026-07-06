package imapclient

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiqi/wispbox/internal/auth"
)

// Mock is an in-memory mailbox used in development mode and tests.
// Every user gets a seeded demo mailbox on first access.
type Mock struct {
	mu    sync.Mutex
	users map[string]*mockMailbox
}

func NewMock() *Mock { return &Mock{users: map[string]*mockMailbox{}} }

type mockMailbox struct {
	order   []string
	folders map[string]*mockFolder
	nextUID uint32
}

type mockFolder struct {
	role     string
	messages []*mockMessage
}

type mockMessage struct {
	msg     *Message
	raw     []byte   // set for appended/delivered messages
	attData [][]byte // seeded attachment payloads by index
}

// mailboxLocked returns (creating if needed) the user's mailbox.
// Callers must hold m.mu.
func (m *Mock) mailboxLocked(email string) *mockMailbox {
	if mb, ok := m.users[email]; ok {
		return mb
	}
	mb := seedMailbox(email)
	m.users[email] = mb
	return mb
}

func (mb *mockMailbox) folderLocked(name string) (*mockFolder, error) {
	f, ok := mb.folders[name]
	if !ok {
		return nil, fmt.Errorf("folder %q does not exist", name)
	}
	return f, nil
}

func (mb *mockMailbox) findLocked(folder string, uid uint32) (*mockFolder, int, error) {
	f, err := mb.folderLocked(folder)
	if err != nil {
		return nil, 0, err
	}
	for i, mm := range f.messages {
		if mm.msg.UID == uid {
			return f, i, nil
		}
	}
	return nil, 0, fmt.Errorf("message %d not found in %s", uid, folder)
}

func (mb *mockMailbox) add(folder string, msg *Message, raw []byte, attData [][]byte) *mockMessage {
	f, ok := mb.folders[folder]
	if !ok {
		f = &mockFolder{role: RoleForFolderName(folder)}
		mb.folders[folder] = f
		mb.order = append(mb.order, folder)
	}
	mb.nextUID++
	msg.UID = mb.nextUID
	msg.Folder = folder
	mm := &mockMessage{msg: msg, raw: raw, attData: attData}
	f.messages = append(f.messages, mm)
	return mm
}

// DeliverLocal places a raw message into a local user's INBOX. The mock SMTP
// sender uses this so dev-mode sends between local users actually "arrive".
func (m *Mock) DeliverLocal(recipient string, raw []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	parsed, err := ParseMessage(raw)
	if err != nil {
		return
	}
	if parsed.Date.IsZero() {
		parsed.Date = time.Now()
	}
	m.mailboxLocked(recipient).add("INBOX", parsed, raw, nil)
}

func (m *Mock) Folders(_ context.Context, creds auth.Credentials) ([]Folder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	mb := m.mailboxLocked(creds.Email)
	var out []Folder
	for _, name := range mb.order {
		f := mb.folders[name]
		var unseen uint32
		for _, mm := range f.messages {
			if !mm.msg.Seen {
				unseen++
			}
		}
		out = append(out, Folder{Name: name, Role: f.role, Total: uint32(len(f.messages)), Unseen: unseen})
	}
	return out, nil
}

func (m *Mock) List(_ context.Context, creds auth.Credentials, folder, cursor string, limit int) (*Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.mailboxLocked(creds.Email).folderLocked(folder)
	if err != nil {
		return nil, err
	}
	return pageOf(f.messages, cursor, limit), nil
}

func (m *Mock) Search(_ context.Context, creds auth.Credentials, folder, query string, limit int) (*Page, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, err := m.mailboxLocked(creds.Email).folderLocked(folder)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(query)
	var matched []*mockMessage
	for _, mm := range f.messages {
		hay := strings.ToLower(mm.msg.Subject + " " + mm.msg.TextBody + " " + addressesText(mm.msg.From) + " " + addressesText(mm.msg.To))
		if strings.Contains(hay, q) {
			matched = append(matched, mm)
		}
	}
	// Search returns a single page (matching the Dovecot adapter and the API,
	// which does not thread a cursor through searches); never advertise a
	// next cursor here or "load more" would append duplicate results.
	page := pageOf(matched, "", limit)
	page.NextCursor = ""
	return page, nil
}

func pageOf(msgs []*mockMessage, cursor string, limit int) *Page {
	limit = clampLimit(limit)
	offset := 0
	if cursor != "" {
		offset, _ = strconv.Atoi(cursor)
	}
	if offset < 0 {
		offset = 0
	}
	// Newest first.
	sorted := append([]*mockMessage(nil), msgs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].msg.Date.After(sorted[j].msg.Date) })

	page := &Page{Total: uint32(len(sorted))}
	if offset > len(sorted) {
		offset = len(sorted)
	}
	end := offset + limit
	if end > len(sorted) {
		end = len(sorted)
	}
	for _, mm := range sorted[offset:end] {
		page.Messages = append(page.Messages, mm.msg.Header)
	}
	if end < len(sorted) {
		page.NextCursor = strconv.Itoa(end)
	}
	return page
}

func (m *Mock) Get(_ context.Context, creds auth.Credentials, folder string, uid uint32) (*Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, i, err := m.mailboxLocked(creds.Email).findLocked(folder, uid)
	if err != nil {
		return nil, err
	}
	cp := *f.messages[i].msg
	return &cp, nil
}

func (m *Mock) Attachment(_ context.Context, creds auth.Credentials, folder string, uid uint32, index int) (*AttachmentContent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, i, err := m.mailboxLocked(creds.Email).findLocked(folder, uid)
	if err != nil {
		return nil, err
	}
	mm := f.messages[i]
	if mm.raw != nil {
		return ExtractAttachment(mm.raw, index)
	}
	if index < 0 || index >= len(mm.msg.Attachments) || index >= len(mm.attData) {
		return nil, fmt.Errorf("attachment %d not found", index)
	}
	meta := mm.msg.Attachments[index]
	return &AttachmentContent{Filename: meta.Filename, MIMEType: meta.MIMEType, Data: mm.attData[index]}, nil
}

func (m *Mock) Move(_ context.Context, creds auth.Credentials, folder string, uid uint32, dest string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mb := m.mailboxLocked(creds.Email)
	f, i, err := mb.findLocked(folder, uid)
	if err != nil {
		return err
	}
	df, err := mb.folderLocked(dest)
	if err != nil {
		return err
	}
	mm := f.messages[i]
	f.messages = append(f.messages[:i], f.messages[i+1:]...)
	mb.nextUID++
	mm.msg.UID = mb.nextUID
	mm.msg.Folder = dest
	df.messages = append(df.messages, mm)
	return nil
}

func (m *Mock) Delete(_ context.Context, creds auth.Credentials, folder string, uid uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, i, err := m.mailboxLocked(creds.Email).findLocked(folder, uid)
	if err != nil {
		return err
	}
	f.messages = append(f.messages[:i], f.messages[i+1:]...)
	return nil
}

func (m *Mock) SetSeen(_ context.Context, creds auth.Credentials, folder string, uid uint32, seen bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, i, err := m.mailboxLocked(creds.Email).findLocked(folder, uid)
	if err != nil {
		return err
	}
	f.messages[i].msg.Seen = seen
	return nil
}

func (m *Mock) Append(_ context.Context, creds auth.Credentials, folder string, raw []byte, seen bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	mb := m.mailboxLocked(creds.Email)
	parsed, err := ParseMessage(raw)
	if err != nil {
		return fmt.Errorf("append: %w", err)
	}
	if parsed.Date.IsZero() {
		parsed.Date = time.Now()
	}
	parsed.Seen = seen
	mb.add(folder, parsed, raw, nil)
	return nil
}

func (m *Mock) AppendReader(ctx context.Context, creds auth.Credentials, folder string, _ int64, raw io.Reader, seen bool) error {
	data, err := io.ReadAll(raw)
	if err != nil {
		return err
	}
	return m.Append(ctx, creds, folder, data, seen)
}

func addressesText(addrs []Address) string {
	var parts []string
	for _, a := range addrs {
		parts = append(parts, a.Name+" "+a.Email)
	}
	return strings.Join(parts, " ")
}
