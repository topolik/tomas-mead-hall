CREATE TABLE plans (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_code TEXT,
    title TEXT NOT NULL,
    detail TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending',
    comment TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    decided_at DATETIME
);

CREATE INDEX idx_plans_status ON plans(status);
CREATE INDEX idx_plans_project ON plans(project_code);
