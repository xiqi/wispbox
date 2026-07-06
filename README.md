# wispbox

Self-hosted email, minus the heavy machinery.

Everything you need to self-host email, wrapped in a clean modern UI.
Perfect for solo developers and startups that want every project to have a proper email address.

It installs on the smallest VPS tier and stays feather-light, leaving your server room to breathe.

## What You Get

| Surface | Where | For |
|---|---|---|
| Webmail | `/` | reading, writing, searching, attachments |
| Admin | `/admin` | domains, mailboxes, aliases, delivery, DNS, certs, queue, logs |
| Setup | `/setup` | first-run setup; gone after the server is ready |
| IMAP | `993` | Apple Mail, Thunderbird, phones, and other clients |
| Submission | `587` | authenticated outbound mail |

The target is a small VPS with a stable public IP. wispbox stays light on
purpose: no Docker, no Redis, no Postgres, no PHP webmail, no Node.js runtime
on the server.

## Before You Install

Use a fresh server if you can.

- Debian 12/13 or Ubuntu 24.04/26.04
- systemd
- 512 MB RAM minimum; 1 GB is nicer
- a public IPv4 address
- ports `25`, `80`, `443`, `587`, and `993` open
- a PTR record for your IP, usually `mail.example.com`

If your host blocks port 25, receiving may still work, but direct outbound
mail will not. Use a relay for sending.

## Install

Release install:

```sh
curl -fsSL https://raw.githubusercontent.com/xiqi/wispbox/main/packaging/install.sh | sudo sh
```

Or build from source:

```sh
git clone https://github.com/xiqi/wispbox.git
cd wispbox
make build
sudo sh packaging/install.sh
```

The installer sets up Postfix, Dovecot, OpenDKIM, `wispboxd`, `wispboxctl`,
systemd, directories, permissions, and the SQLite control database. Re-running
it upgrades binaries and keeps your data.

## First Run

Open:

```text
https://your-server/setup
```

The wizard walks you through everything, in order:

- create the admin account
- set the mail hostname
- add the first domain
- choose direct sending or a relay
- publish DNS records
- issue a Let's Encrypt certificate
- create the first mailbox
- send a test email

You can close the tab halfway through. `/setup` resumes where you left off.
After completion it redirects to `/admin`.

## Sign In

Mailbox users use webmail:

```text
https://mail.example.com/
```

Admins use:

```text
https://mail.example.com/admin
```

Admin accounts use local usernames, not mailbox addresses. That is intentional:
the person who manages the server is not automatically every mailbox user.

## Sending Mail

wispbox supports two outbound modes:

- **Direct**: your server talks to recipient MX servers on port 25. That is
  normal server-to-server SMTP, not user submission. STARTTLS is used when the
  peer offers it. Free and independent, but you need a good PTR record, open
  egress, and IP reputation.
- **Relay**: outbound mail goes through a provider such as SES, Postmark,
  Mailgun, SMTP2GO, Resend, or any SMTP server, usually with STARTTLS on 587
  or implicit TLS on 465. Easier delivery, small bill, third party in the loop.

A practical default: receive mail directly, send through a relay until your
server has earned trust.

wispbox never silently falls back from relay to direct. If a relay breaks,
mail stays in the queue where you can see it, retry it, or delete it.

## Daily Commands

```sh
wispboxctl status
wispboxctl doctor
wispboxctl backup create /var/backups/wispbox-$(date +%F).tar.gz
wispboxctl backup restore /var/backups/wispbox-2026-07-05.tar.gz
wispboxctl cert renew --hostname mail.example.com
```

Useful logs:

```sh
journalctl -u wispboxd -f
journalctl -u postfix -f
journalctl -u dovecot -f
journalctl -u opendkim -f
```

Generated mail config lives under `/etc/wispbox/generated/`. Do not edit it by
hand; the database is the source of truth. To inspect what wispbox will write:

```sh
sudo -u wispbox wispboxd config render
sudo -u wispbox wispboxd config validate
```

## When Something Feels Off

Start boring. Boring usually wins.

```sh
wispboxctl doctor
systemctl status wispboxd
journalctl -u wispboxd -n 80
```

Common culprits:

- port `80` or `443` is already taken by nginx, Apache, or Caddy
- DNS has not propagated yet
- your VPS provider blocks port `25`
- the PTR record does not match the mail hostname
- relay credentials are wrong
- a webmail session expired after `wispboxd` restarted

Certificates renew automatically 30 days before expiry. A failed renewal keeps
the last working certificate in place and retries with backoff.

## Try It Locally

```sh
make dev
```

Open <http://localhost:8080/>.

Seeded logins:

| Surface | Login |
|---|---|
| Webmail | `demo@example.com` / `wispbox-demo` |
| Admin | `admin` / `wispbox-admin` |

Development mode uses mock Postfix, Dovecot, DNS, ACME, systemd, IMAP, and
SMTP adapters. It keeps data in `./devdata` and never touches system paths.

Developer notes live in [docs/development.md](docs/development.md).

## License

MIT.
