package imapclient

import (
	"fmt"
	"strings"
	"time"
)

// seedMailbox builds the demo mailbox every dev-mode user starts with.
func seedMailbox(email string) *mockMailbox {
	mb := &mockMailbox{folders: map[string]*mockFolder{}}
	now := time.Now()
	name := strings.Split(email, "@")[0]

	// Standard folders in display order.
	for _, f := range []string{"INBOX", "Sent", "Drafts", "Junk", "Trash"} {
		mb.folders[f] = &mockFolder{role: RoleForFolderName(f)}
		mb.order = append(mb.order, f)
	}

	seed := func(folder string, msg *Message, attData [][]byte) {
		mb.add(folder, msg, nil, attData)
	}

	seed("INBOX", &Message{
		Header: Header{
			From: []Address{{Name: "wispbox", Email: "hello@wispbox.dev"}}, To: []Address{{Email: email}},
			Subject: "Welcome to wispbox 🎉", Date: now.Add(-15 * time.Minute), Size: 4210,
		},
		TextBody: "Welcome to your new mailbox!\n\nThis is a demo message from development mode. Everything you see here is served by the mock IMAP adapter — no real mail server involved.\n\nEnjoy,\nThe wispbox demo seed",
		HTMLBody: `<div style="font-family: sans-serif; max-width: 560px">
<h2 style="color:#7c6cf6">Welcome to your new mailbox</h2>
<p>This is a <strong>demo message</strong> from development mode. Everything you see here is served by the mock IMAP adapter — no real mail server involved.</p>
<blockquote style="border-left:3px solid #7c6cf6; padding-left:12px; color:#666">Beautiful self-hosted email for small teams.</blockquote>
<p>Enjoy,<br>The wispbox demo seed</p></div>`,
	}, nil)

	invoicePDF := []byte("%PDF-1.4\n% wispbox demo invoice\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Root 1 0 R >>\n%%EOF\n")
	seed("INBOX", &Message{
		Header: Header{
			From: []Address{{Name: "Nimbus Studio", Email: "billing@nimbus.studio"}}, To: []Address{{Email: email}},
			Subject: "Invoice #2041 — June hosting", Date: now.Add(-3 * time.Hour), Size: 18234,
			HasAttachments: true,
		},
		TextBody: fmt.Sprintf("Hi %s,\n\nPlease find attached the invoice for June hosting (EUR 12.00).\nPayment is due within 14 days.\n\nBest,\nNimbus Studio Billing", name),
		Attachments: []AttachmentMeta{
			{Index: 0, Filename: "invoice-2041.pdf", MIMEType: "application/pdf", Size: int64(len(invoicePDF))},
		},
	}, [][]byte{invoicePDF})

	seed("INBOX", &Message{
		Header: Header{
			From: []Address{{Name: "Mira Chen", Email: "mira@example.org"}}, To: []Address{{Email: email}},
			Subject: "Re: dinner on Friday?", Date: now.Add(-26 * time.Hour), Seen: true, Answered: true, Size: 1830,
		},
		TextBody: "Friday works! Let's do 19:30 at the ramen place near the station.\n\nOn Wed, you wrote:\n> Are we still on for Friday?\n> I could also do Saturday.\n",
	}, nil)

	seed("INBOX", &Message{
		Header: Header{
			From: []Address{{Name: "forge.dev", Email: "notifications@forge.dev"}}, To: []Address{{Email: email}},
			Subject: "[wispbox/wispbox] PR #42 merged: Per-domain certificates", Date: now.Add(-2 * 24 * time.Hour), Seen: true, Size: 6120,
		},
		TextBody: "Merged #42 into main.\n\nPer-domain certificates\n- SNI selection for HTTPS, SMTP and IMAP\n- automatic renewal with backoff\n\nView it on forge.dev.",
	}, nil)

	seed("INBOX", &Message{
		Header: Header{
			From: []Address{{Name: "The Sidecar Weekly", Email: "letter@sidecar.news"}}, To: []Address{{Email: email}},
			Subject: "Issue 118: Small servers are back", Date: now.Add(-3 * 24 * time.Hour), Size: 45230,
		},
		TextBody: "SMALL SERVERS ARE BACK\n\nThis week: why 512MB VPSs are enough for email, the return of Maildir, and a love letter to boring infrastructure.\n\n(If you can read this, your client blocked our tracking pixel. Good for you.)",
		HTMLBody: `<div style="font-family: Georgia, serif; max-width:560px">
<h1 style="font-size:22px">Small servers are back</h1>
<img src="https://tracker.sidecar.news/pixel.gif?u=12345" width="1" height="1" alt="">
<p>This week: why 512MB VPSs are enough for email, the return of Maildir, and a love letter to boring infrastructure.</p>
<img src="https://cdn.sidecar.news/issue-118-hero.jpg" alt="A tiny server rack" width="520">
<p style="color:#888; font-size:12px">You are receiving this because you subscribed at sidecar.news.</p></div>`,
	}, nil)

	seed("INBOX", &Message{
		Header: Header{
			From: []Address{{Name: "Root", Email: "root@vps-3021.host"}}, To: []Address{{Email: email}},
			Subject: "cron: nightly backup finished (2.1 GB, 41s)", Date: now.Add(-4 * 24 * time.Hour), Seen: true, Size: 980,
		},
		TextBody: "Nightly backup finished successfully.\n\n  source: /var/lib/wispbox\n  size:   2.1 GB\n  took:   41s\n  target: s3://backups/vps-3021\n",
	}, nil)

	seed("Sent", &Message{
		Header: Header{
			From: []Address{{Email: email}}, To: []Address{{Name: "Mira Chen", Email: "mira@example.org"}},
			Subject: "dinner on Friday?", Date: now.Add(-28 * time.Hour), Seen: true, Size: 640,
		},
		TextBody: "Are we still on for Friday?\nI could also do Saturday.",
	}, nil)

	seed("Sent", &Message{
		Header: Header{
			From: []Address{{Email: email}}, To: []Address{{Name: "Nimbus Studio", Email: "billing@nimbus.studio"}},
			Subject: "Re: Invoice #2018", Date: now.Add(-9 * 24 * time.Hour), Seen: true, Size: 512,
		},
		TextBody: "Paid today via bank transfer, reference WISP-2018. Thanks!",
	}, nil)

	seed("Drafts", &Message{
		Header: Header{
			From: []Address{{Email: email}}, To: []Address{{Email: "team@example.org"}},
			Subject: "Draft: migration plan", Date: now.Add(-5 * time.Hour), Seen: true, Size: 720,
		},
		TextBody: "Rough plan for moving mail off the old box:\n\n1. Stand up wispbox on the new VPS\n2. Sync Maildirs with rsync\n3. Flip MX after 48h\n\n(unfinished)",
	}, nil)

	seed("Junk", &Message{
		Header: Header{
			From: []Address{{Name: "Prize Dept", Email: "winner@lottery-intl.example"}}, To: []Address{{Email: email}},
			Subject: "FINAL NOTICE: your prize expires TODAY", Date: now.Add(-30 * time.Hour), Size: 2100,
		},
		TextBody: "Congratulations! You have been selected to receive USD 1,000,000. Reply with your bank details to claim.",
	}, nil)

	seed("Trash", &Message{
		Header: Header{
			From: []Address{{Name: "Calendar", Email: "no-reply@calendar.example"}}, To: []Address{{Email: email}},
			Subject: "Reminder: standup in 15 minutes", Date: now.Add(-52 * time.Hour), Seen: true, Size: 830,
		},
		TextBody: "Standup starts at 09:45. Join at meet.example/standup.",
	}, nil)

	return mb
}
