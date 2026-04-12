CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    detail TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'info',
    created_at TEXT DEFAULT (datetime('now'))
);
CREATE INDEX idx_audit_type ON audit_log(event_type);
CREATE INDEX idx_audit_severity ON audit_log(severity);
