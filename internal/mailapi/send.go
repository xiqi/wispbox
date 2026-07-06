package mailapi

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/xiqi/wispbox/internal/api/httpjson"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/security"
)

const maxSendBytes = 30 << 20 // whole submission incl. attachments

var (
	reScriptBlock = regexp.MustCompile(`(?is)<script[^>]*>.*?</\s*script\s*>`)
	reStyleBlock  = regexp.MustCompile(`(?is)<style[^>]*>.*?</\s*style\s*>`)
)

// allowedSenders returns every address this mailbox may use as From:
// its own address plus enabled aliases that forward to it.
func (h *Handlers) allowedSenders(r *http.Request, mc *mailCtx) (map[string]bool, error) {
	allowed := map[string]bool{mc.Mailbox.Email: true}
	sources, err := h.Store.ListAliasSourcesForDestination(r.Context(), mc.Mailbox.Email)
	if err != nil {
		return nil, err
	}
	for _, s := range sources {
		if !strings.HasPrefix(s, "@") { // catch-alls never grant send rights
			allowed[s] = true
		}
	}
	return allowed, nil
}

// resolveFrom validates the requested From against the allowed set.
func (h *Handlers) resolveFrom(r *http.Request, mc *mailCtx, requested string) (string, error) {
	requested = strings.ToLower(strings.TrimSpace(requested))
	if requested == "" {
		return mc.Mailbox.Email, nil
	}
	allowed, err := h.allowedSenders(r, mc)
	if err != nil {
		return "", err
	}
	if !allowed[requested] {
		return "", fmt.Errorf("you can only send as %s or an alias that forwards to you", mc.Mailbox.Email)
	}
	return requested, nil
}

// parseSendRequest accepts multipart/form-data (with attachments) or JSON.
func (h *Handlers) parseSendRequest(w http.ResponseWriter, r *http.Request) (*sendRequest, error) {
	ct := r.Header.Get("Content-Type")
	req := &sendRequest{}
	if strings.HasPrefix(ct, "multipart/form-data") {
		r.Body = http.MaxBytesReader(w, r.Body, maxSendBytes)
		if err := r.ParseMultipartForm(4 << 20); err != nil {
			return nil, fmt.Errorf("upload too large or malformed (max 25 MB per attachment)")
		}
		req.From = r.FormValue("from")
		req.To = r.FormValue("to")
		req.CC = r.FormValue("cc")
		req.BCC = r.FormValue("bcc")
		req.Subject = r.FormValue("subject")
		req.Body = r.FormValue("body")
		req.HTMLBody = r.FormValue("html_body")
		req.InReplyTo = r.FormValue("in_reply_to")
		if r.MultipartForm != nil {
			for _, fh := range r.MultipartForm.File["attachments"] {
				if err := security.CheckOutgoingAttachment(fh.Filename, fh.Size); err != nil {
					return nil, err
				}
				f, err := fh.Open()
				if err != nil {
					return nil, err
				}
				data, err := io.ReadAll(io.LimitReader(f, security.MaxAttachmentSize+1))
				f.Close()
				if err != nil {
					return nil, err
				}
				if int64(len(data)) > security.MaxAttachmentSize {
					return nil, fmt.Errorf("attachment %s exceeds the 25 MB limit", fh.Filename)
				}
				req.Atts = append(req.Atts, OutgoingAttachment{
					Filename:    fh.Filename,
					ContentType: fh.Header.Get("Content-Type"),
					Data:        data,
				})
			}
		}
		return req, nil
	}
	if err := httpjson.Decode(w, r, req, 1<<20); err != nil {
		return nil, err
	}
	return req, nil
}

type sendRequest struct {
	From      string `json:"from"`
	To        string `json:"to"`
	CC        string `json:"cc"`
	BCC       string `json:"bcc"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`      // text/plain body (or the plain fallback)
	HTMLBody  string `json:"html_body"` // optional rich body from the editor
	InReplyTo string `json:"in_reply_to"`

	Atts []OutgoingAttachment `json:"-"`
}

func (h *Handlers) send(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	req, err := h.parseSendRequest(w, r)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	h.deliver(w, r, mc, req)
}

func (h *Handlers) deliver(w http.ResponseWriter, r *http.Request, mc *mailCtx, req *sendRequest) {
	from, err := h.resolveFrom(r, mc, req.From)
	if err != nil {
		httpjson.Error(w, http.StatusForbidden, err)
		return
	}
	to, err := parseAddressField(req.To)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	cc, err := parseAddressField(req.CC)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	bcc, err := parseAddressField(req.BCC)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if len(to)+len(cc)+len(bcc) == 0 {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("add at least one recipient"))
		return
	}

	// Rich body: sanitize the editor's HTML server-side (never trust the
	// client) and guarantee a plain-text alternative for the multipart part.
	htmlBody := ""
	textBody := req.Body
	if strings.TrimSpace(req.HTMLBody) != "" {
		htmlBody = security.SanitizeOutgoingHTML(req.HTMLBody)
		if strings.TrimSpace(textBody) == "" {
			textBody = htmlToText(htmlBody)
		}
	}

	out := &Outgoing{
		From: from, To: to, CC: cc, BCC: bcc,
		Subject: strings.TrimSpace(req.Subject), TextBody: textBody, HTMLBody: htmlBody,
		InReplyTo: strings.Trim(req.InReplyTo, "<>"),
		Atts:      req.Atts,
	}
	raw, err := BuildMIME(out)
	if err != nil {
		httpjson.Error(w, http.StatusInternalServerError, fmt.Errorf("could not build the message: %w", err))
		return
	}
	if err := h.SMTP.Send(r.Context(), mc.Creds, from, out.AllRecipients(), raw); err != nil {
		_ = h.Store.AppendServiceEvent(r.Context(), db.ServiceEvent{
			Service: "wispboxd", EventType: "send_error", Status: "error",
			Message: fmt.Sprintf("%s: %v", from, err),
		})
		httpjson.Error(w, http.StatusBadGateway, err)
		return
	}
	if err := h.IMAP.Append(r.Context(), mc.Creds, "Sent", raw, true); err != nil {
		h.Log.Warn("append to Sent failed", "error", err)
	}
	httpjson.Write(w, http.StatusOK, nil)
}

// reply loads the original to prefill threading headers, then delivers.
func (h *Handlers) reply(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	var req struct {
		ID       string `json:"id"`
		Body     string `json:"body"`
		HTMLBody string `json:"html_body"`
		ReplyAll bool   `json:"reply_all"`
	}
	if err := httpjson.Decode(w, r, &req, 4<<20); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	folder, uid, err := decodeMessageID(req.ID)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	orig, err := h.IMAP.Get(r.Context(), mc.Creds, folder, uid)
	if err != nil {
		httpjson.Error(w, http.StatusNotFound, fmt.Errorf("original message not found"))
		return
	}

	replyTo := orig.ReplyTo
	if len(replyTo) == 0 {
		replyTo = orig.From
	}
	var to []string
	for _, a := range replyTo {
		to = append(to, a.Email)
	}
	var cc []string
	if req.ReplyAll {
		for _, a := range append(orig.To, orig.CC...) {
			if a.Email != mc.Mailbox.Email && a.Email != "" {
				cc = append(cc, a.Email)
			}
		}
	}
	subject := orig.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	h.deliver(w, r, mc, &sendRequest{
		To: strings.Join(to, ", "), CC: strings.Join(cc, ", "),
		Subject: subject, Body: req.Body, HTMLBody: req.HTMLBody,
		InReplyTo: orig.MessageID,
	})
}

// forward re-sends the original body (quoted) and its attachments, honoring
// any recipient and subject edits the user made.
func (h *Handlers) forward(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	var req struct {
		ID       string `json:"id"`
		To       string `json:"to"`
		CC       string `json:"cc"`
		BCC      string `json:"bcc"`
		Subject  string `json:"subject"`
		Body     string `json:"body"`      // includes the client-built forwarded block
		HTMLBody string `json:"html_body"` // optional rich version
	}
	if err := httpjson.Decode(w, r, &req, 4<<20); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	folder, uid, err := decodeMessageID(req.ID)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	orig, err := h.IMAP.Get(r.Context(), mc.Creds, folder, uid)
	if err != nil {
		httpjson.Error(w, http.StatusNotFound, fmt.Errorf("original message not found"))
		return
	}

	// Re-attach the original's attachments (the quoted body itself is built
	// by the client, which has the original in both text and HTML form).
	var atts []OutgoingAttachment
	for _, meta := range orig.Attachments {
		content, err := h.IMAP.Attachment(r.Context(), mc.Creds, folder, uid, meta.Index)
		if err != nil {
			httpjson.Error(w, http.StatusBadGateway, fmt.Errorf("could not fetch attachment %s", meta.Filename))
			return
		}
		atts = append(atts, OutgoingAttachment{Filename: content.Filename, ContentType: content.MIMEType, Data: content.Data})
	}

	// Honor an edited subject; otherwise default to "Fwd: <original>".
	subject := strings.TrimSpace(req.Subject)
	if subject == "" {
		subject = orig.Subject
		if !strings.HasPrefix(strings.ToLower(subject), "fwd:") {
			subject = "Fwd: " + subject
		}
	}

	h.deliver(w, r, mc, &sendRequest{
		To: req.To, CC: req.CC, BCC: req.BCC, Subject: subject,
		Body: req.Body, HTMLBody: req.HTMLBody, Atts: atts,
	})
}

// htmlToText derives a readable plain-text fallback from HTML for the
// text/plain part of a multipart/alternative message. It is intentionally
// simple: block tags become newlines, list items get a bullet, links keep
// their text, and remaining tags are stripped. The client normally supplies
// its own plain text, so this is only a safety net.
func htmlToText(html string) string {
	s := html
	// RE2 has no backreferences, so strip script and style blocks separately.
	s = reScriptBlock.ReplaceAllString(s, "")
	s = reStyleBlock.ReplaceAllString(s, "")
	s = regexp.MustCompile(`(?i)<br\s*/?>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`(?i)<li[^>]*>`).ReplaceAllString(s, "\n• ")
	s = regexp.MustCompile(`(?i)</(p|div|h[1-6]|blockquote|li|tr|ul|ol)>`).ReplaceAllString(s, "\n")
	s = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(s, "")
	s = html2textUnescape(s)
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func html2textUnescape(s string) string {
	r := strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&apos;", "'",
	)
	return r.Replace(s)
}
