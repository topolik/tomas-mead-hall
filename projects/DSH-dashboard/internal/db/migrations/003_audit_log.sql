ALTER TABLE oauth2_clients ADD COLUMN last_used_at DATETIME;
ALTER TABLE oauth2_clients ADD COLUMN last_used_ip TEXT;

CREATE TABLE IF NOT EXISTS api_audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id  TEXT NOT NULL REFERENCES oauth2_clients(client_id),
    method     TEXT NOT NULL,
    path       TEXT NOT NULL,
    remote_ip  TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_audit_client  ON api_audit_log(client_id);
CREATE INDEX IF NOT EXISTS idx_audit_created ON api_audit_log(created_at DESC);
