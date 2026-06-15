CREATE TABLE IF NOT EXISTS config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    username      TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS totp_credentials (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    secret     TEXT NOT NULL,
    verified   INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS passkey_credentials (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_id TEXT NOT NULL UNIQUE,
    public_key    BLOB NOT NULL,
    sign_count    INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sessions (
    id         TEXT PRIMARY KEY,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    data       TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    expires_at DATETIME NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user   ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expiry ON sessions(expires_at);

CREATE TABLE IF NOT EXISTS oauth2_clients (
    client_id          TEXT PRIMARY KEY,
    client_secret_hash TEXT NOT NULL,
    name               TEXT NOT NULL,
    created_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    revoked_at         DATETIME
);

CREATE TABLE IF NOT EXISTS backlog_items (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    text       TEXT NOT NULL,
    priority   TEXT NOT NULL CHECK(priority IN ('Q1','Q2','Q3','Q4')),
    status     TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','in_progress','done','parked')),
    added_date DATE NOT NULL DEFAULT (date('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS projects (
    code          TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    status        TEXT NOT NULL DEFAULT 'Ideation',
    priority      TEXT NOT NULL DEFAULT 'Q2',
    lead          TEXT NOT NULL DEFAULT '',
    current_phase TEXT NOT NULL DEFAULT 'Ideation',
    last_updated  DATE NOT NULL DEFAULT (date('now'))
);

CREATE TABLE IF NOT EXISTS notifications (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    project_code TEXT REFERENCES projects(code) ON DELETE SET NULL,
    message      TEXT NOT NULL,
    type         TEXT NOT NULL DEFAULT 'info' CHECK(type IN ('action_needed','info')),
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    dismissed_at DATETIME
);

CREATE INDEX IF NOT EXISTS idx_notifications_active ON notifications(dismissed_at);

CREATE TABLE IF NOT EXISTS webauthn_sessions (
    id         TEXT PRIMARY KEY,
    user_id    INTEGER REFERENCES users(id) ON DELETE CASCADE,
    data       TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    expires_at DATETIME NOT NULL
);
