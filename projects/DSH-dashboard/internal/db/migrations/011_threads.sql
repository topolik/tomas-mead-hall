-- Threads: durable M:N discussions attachable to notifications/plans/projects.
-- First consumer: GML processed-tracking (resolved thread on a dismissed
-- insight notification => distill skips re-processing it).
CREATE TABLE threads (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subject TEXT NOT NULL,
    ref_type TEXT CHECK(ref_type IN ('notification','plan','project')),
    ref_id TEXT,
    status TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open','resolved')),
    created_by TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_threads_ref ON threads(ref_type, ref_id);

CREATE TABLE thread_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    thread_id INTEGER NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    author TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_thread_messages_thread ON thread_messages(thread_id);
