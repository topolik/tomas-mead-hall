CREATE TABLE IF NOT EXISTS audit_log (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    event      TEXT NOT NULL,
    actor      TEXT NOT NULL DEFAULT '',
    remote_ip  TEXT NOT NULL DEFAULT '',
    detail     TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_actor   ON audit_log(actor);

-- Migrate existing API audit entries into the unified log
INSERT INTO audit_log(event, actor, remote_ip, detail, created_at)
SELECT 'api_call', client_id, remote_ip, method || ' ' || path, created_at
FROM api_audit_log;
