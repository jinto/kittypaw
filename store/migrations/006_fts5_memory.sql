CREATE VIRTUAL TABLE IF NOT EXISTS execution_fts USING fts5(
    skill_id, skill_name, result_summary,
    content='execution_history', content_rowid='id'
);
CREATE TRIGGER IF NOT EXISTS execution_fts_insert AFTER INSERT ON execution_history BEGIN
    INSERT INTO execution_fts(rowid, skill_id, skill_name, result_summary)
    VALUES (new.id, new.skill_id, new.skill_name, new.result_summary);
END;
CREATE TRIGGER IF NOT EXISTS execution_fts_delete BEFORE DELETE ON execution_history BEGIN
    INSERT INTO execution_fts(execution_fts, rowid, skill_id, skill_name, result_summary)
    VALUES ('delete', old.id, old.skill_id, old.skill_name, old.result_summary);
END;
INSERT INTO execution_fts(execution_fts) VALUES ('rebuild');
