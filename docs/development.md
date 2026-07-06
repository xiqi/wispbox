# Development

wispbox should stay easy to hold in your head. One daemon, one frontend, one
SQLite control database, and small adapters around anything that touches the
host.

## Setup

You need:

- Go 1.25+
- Node 22+
- npm
- make

Run the full product locally:

```sh
make dev
```

That is the same as:

```sh
go run ./cmd/wispboxd serve --dev
```

Dev mode listens on `:8080`, stores everything in `./devdata`, seeds demo
accounts, and uses mocks for Postfix, Dovecot, DNS, ACME, systemd, IMAP, and
SMTP. No root. No privileged ports. No surprise edits to your machine.

To reset the world:

```sh
rm -rf devdata
```

To walk through setup instead of using seeded data:

```sh
go run ./cmd/wispboxd serve --dev --seed=false
```

## Frontend Loop

The Go daemon serves the built frontend from `web/dist`.

For React hot reload, run Vite beside it:

```sh
make dev
```

```sh
cd web
npm run dev
```

Open the Vite URL. API calls proxy to `localhost:8080`.

Before you expect `make dev` to serve frontend changes directly, rebuild:

```sh
cd web
npm run build
```

## Commands

```sh
make build   # frontend build, then wispboxd and wispboxctl
make test    # frontend build, then go test ./...
make lint    # frontend build, go vet, then TypeScript check
make clean   # remove local build output and devdata
```

Tests should be quick and hermetic. If a test needs the host, it probably
needs a mock adapter instead.

## Repo Map

```text
cmd/wispboxd/       daemon and operator subcommands
cmd/wispboxctl/     status, doctor, certs, config, backups
internal/api/       app wiring, HTTP servers, embedded frontend
internal/admin/     admin API and server-management operations
internal/mailapi/   webmail API
internal/setup/     first-run wizard API
internal/db/        SQLite migrations and typed store
internal/configgen/ database -> mail-server config
internal/postfix/   Postfix renderer
internal/dovecot/   Dovecot renderer
internal/certs/     certificate state and renewal
internal/security/  sessions, CSRF, rate limits, sanitizing
web/                React/Vite UI
packaging/          installer, systemd unit, mail templates
docs/               this file
```

## The Important Shape

`wispboxd` owns HTTPS, the REST APIs, the UI, certificates, the SQLite control
database, and generated config.

Postfix and Dovecot own mail transport and storage.

That line matters. If `wispboxd` stops, management pauses, but mail can keep
flowing on the last good generated configuration.

Host effects sit behind interfaces. Production adapters call systemd,
journald, Postfix, Dovecot, DNS, ACME, IMAP, and SMTP. Dev/test adapters
pretend to. Adapter selection belongs in `internal/api/app.go`; keep it
there so the rest of the code stays ordinary.

Config generation has one rule: render and validate everything first, write
atomically only after that, reload services last. A bad change must never
overwrite a working mail config.

## Adding Backend Work

Follow the existing path before inventing a new one.

For an admin feature:

1. put the operation on `internal/admin.Core` if it changes server state
2. add or reuse store methods in `internal/db`
3. register the handler in `internal/admin`
4. use `httpjson.Decode`, `httpjson.Write`, and `httpjson.Fail`
5. call config regeneration after mail config changes
6. add an `httptest` test with mock adapters

For webmail, mirror the same pattern in `internal/mailapi`. For setup, use
`internal/setup` and keep the wizard resumable.

Mutating authenticated endpoints get CSRF checks from the middleware. Audit
logging is explicit; log meaningful admin actions after they succeed.

## Adding UI Work

Keep the UI quiet and useful.

- write `wispbox` lowercase
- remove filler copy
- prefer clear labels over explanations
- do not add a card just because there is space
- keep controls stable across desktop and mobile widths
- use existing components in `web/src/components`
- rebuild `web/dist` before checking the Go-served app

The product is a tool, not a brochure. Every sentence should earn its spot.

## Tests Worth Keeping

Coverage should protect behavior, not decorate percentages.

Good targets:

- SQLite migrations and store rules
- auth separation between admin and webmail
- CSRF and authorization
- DNS record generation and checking
- delivery policy resolution
- relay validation
- certificate state, backoff, and SNI selection
- Postfix/Dovecot config rendering
- config generation fail-safety
- mail API flows with mock IMAP/SMTP
- setup gating and completion

Run:

```sh
make test
```

For a quick live smoke:

```sh
go run ./cmd/wispboxd serve --dev
curl -s localhost:8080/api/setup/status
```

## Release Notes

`make release` cross-compiles Linux `amd64` and `arm64` binaries into `dist/`.
The GitHub release workflow runs tests first, builds artifacts, generates
checksums, and publishes on `v*` tags.

Node is build-time only. The release binary embeds the compiled frontend.

## Documentation Rule

There are only two docs:

- `README.md` for using wispbox
- `docs/development.md` for changing wispbox

If a new page feels tempting, first ask whether the reader would rather find
that answer in one of these two places. They usually would.
