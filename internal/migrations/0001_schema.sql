CREATE TABLE admins (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    username           TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash      TEXT    NOT NULL,
    webauthn_handle    TEXT    NOT NULL DEFAULT '',
    two_factor_enabled INTEGER NOT NULL DEFAULT 0,
    encrypted_totp_secret TEXT NOT NULL DEFAULT '',
    created_at         TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at         TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE domains (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    mail_hostname TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    status        TEXT    NOT NULL DEFAULT 'pending',
    created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE mailboxes (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id     INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    local_part    TEXT    NOT NULL COLLATE NOCASE,
    email         TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT    NOT NULL,
    webauthn_handle TEXT  NOT NULL DEFAULT '',
    two_factor_enabled INTEGER NOT NULL DEFAULT 0,
    encrypted_totp_secret TEXT NOT NULL DEFAULT '',
    encrypted_passkey_password TEXT NOT NULL DEFAULT '',
    quota_mb      INTEGER NOT NULL DEFAULT 0,
    enabled       INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at    TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (domain_id, local_part)
);

CREATE INDEX idx_mailboxes_domain ON mailboxes(domain_id);

CREATE TABLE aliases (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id    INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    source       TEXT    NOT NULL COLLATE NOCASE,
    destination  TEXT    NOT NULL COLLATE NOCASE,
    is_catch_all INTEGER NOT NULL DEFAULT 0,
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (source, destination)
);

CREATE INDEX idx_aliases_domain ON aliases(domain_id);

CREATE TABLE outbound_relays (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT    NOT NULL UNIQUE,
    provider           TEXT    NOT NULL,
    host               TEXT    NOT NULL,
    port               INTEGER NOT NULL,
    username           TEXT    NOT NULL DEFAULT '',
    encrypted_password TEXT    NOT NULL DEFAULT '',
    tls_mode           TEXT    NOT NULL DEFAULT 'starttls',
    enabled            INTEGER NOT NULL DEFAULT 1,
    created_at         TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at         TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE outbound_policies (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    scope_type TEXT    NOT NULL,
    scope_id   INTEGER NOT NULL DEFAULT 0,
    mode       TEXT    NOT NULL,
    relay_id   INTEGER REFERENCES outbound_relays(id) ON DELETE SET NULL,
    enabled    INTEGER NOT NULL DEFAULT 1,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    UNIQUE (scope_type, scope_id)
);

INSERT INTO outbound_policies (scope_type, scope_id, mode) VALUES ('global', 0, 'direct');

CREATE TABLE certificates (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    domain_id       INTEGER REFERENCES domains(id) ON DELETE CASCADE,
    hostname        TEXT    NOT NULL UNIQUE COLLATE NOCASE,
    status          TEXT    NOT NULL DEFAULT 'pending',
    challenge_type  TEXT    NOT NULL DEFAULT 'http-01',
    cert_path       TEXT    NOT NULL DEFAULT '',
    key_path        TEXT    NOT NULL DEFAULT '',
    not_before      TEXT,
    not_after       TEXT,
    last_renewed_at TEXT,
    renew_after     TEXT,
    last_error      TEXT    NOT NULL DEFAULT '',
    created_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at      TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX idx_certificates_domain ON certificates(domain_id);

CREATE TABLE sessions (
    id         TEXT    PRIMARY KEY,
    user_type  TEXT    NOT NULL,
    user_id    INTEGER NOT NULL,
    expires_at TEXT    NOT NULL,
    created_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX idx_sessions_expiry ON sessions(expires_at);

CREATE TABLE passkeys (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    user_type            TEXT    NOT NULL,
    user_id              INTEGER NOT NULL,
    rp_id                TEXT    NOT NULL,
    credential_id        TEXT    NOT NULL,
    name                 TEXT    NOT NULL DEFAULT '',
    encrypted_credential TEXT    NOT NULL,
    created_at           TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    updated_at           TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    last_used_at         TEXT    NOT NULL DEFAULT '',
    UNIQUE (rp_id, credential_id)
);

CREATE INDEX idx_passkeys_user ON passkeys(user_type, user_id, rp_id);

CREATE TABLE auth_challenges (
    id           TEXT    PRIMARY KEY,
    user_type    TEXT    NOT NULL,
    user_id      INTEGER NOT NULL DEFAULT 0,
    kind         TEXT    NOT NULL,
    session_data TEXT    NOT NULL,
    created_at   TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
    expires_at   TEXT    NOT NULL
);

CREATE INDEX idx_auth_challenges_expiry ON auth_challenges(expires_at);

CREATE TABLE audit_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_type  TEXT    NOT NULL,
    actor_id    INTEGER NOT NULL DEFAULT 0,
    action      TEXT    NOT NULL,
    target_type TEXT    NOT NULL DEFAULT '',
    target_id   TEXT    NOT NULL DEFAULT '',
    ip          TEXT    NOT NULL DEFAULT '',
    created_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX idx_audit_logs_created ON audit_logs(created_at);

CREATE TABLE service_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    service    TEXT NOT NULL,
    event_type TEXT NOT NULL,
    status     TEXT NOT NULL,
    message    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE INDEX idx_service_events_created ON service_events(created_at);

CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

INSERT INTO settings (key, value) VALUES ('initialized', 'false');
