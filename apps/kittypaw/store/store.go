package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jinto/gopaw/core"
	_ "modernc.org/sqlite"
)

// ---------------------------------------------------------------------------
// DTO structs
// ---------------------------------------------------------------------------

// AgentSummary is a lightweight listing of an agent with its turn count.
type AgentSummary struct {
	AgentID   string
	TurnCount int
	CreatedAt string
	UpdatedAt string
}

// ExecutionRecord captures one skill execution for history/analysis.
type ExecutionRecord struct {
	ID            int64
	SkillID       string
	SkillName     string
	StartedAt     string
	FinishedAt    string
	DurationMs    int64
	InputParams   string
	ResultSummary string
	Success       bool
	RetryCount    int
	UsageJSON     string
}

// ExecutionStats is an aggregated daily summary.
type ExecutionStats struct {
	TotalRuns   int
	Successful  int
	Failed      int
	AutoRetries int
	TotalTokens int64
}

// KeyValue is a generic key-value pair used for user context listings.
type KeyValue struct {
	Key   string
	Value string
}

// UserIdentity links a global user to a specific channel identity.
type UserIdentity struct {
	Channel       string
	ChannelUserID string
	CreatedAt     string
}

// Checkpoint is a named snapshot of conversation progress.
type Checkpoint struct {
	ID        int64
	AgentID   string
	Label     string
	ConvRowID int64
	CreatedAt string
}

// SkillFix records a code correction applied to a skill.
type SkillFix struct {
	ID        int64
	SkillID   string
	ErrorMsg  string
	OldCode   string
	NewCode   string
	Applied   bool
	CreatedAt string
}

// FilePermissionRule controls file access for a workspace.
type FilePermissionRule struct {
	ID          string
	WorkspaceID string
	PathPattern string
	IsException bool
	CanRead     bool
	CanWrite    bool
	CanDelete   bool
	CreatedAt   string
}

// NetworkPermissionRule controls network access for a workspace.
type NetworkPermissionRule struct {
	ID             string
	WorkspaceID    string
	DomainPattern  string
	AllowedMethods string
	CreatedAt      string
}

// GlobalPath is a globally permitted filesystem path.
type GlobalPath struct {
	ID         string
	Path       string
	AccessType string
	CreatedAt  string
}

// ProfileMeta stores metadata about a switchable agent profile.
type ProfileMeta struct {
	ID             string
	Description    string
	EquippedSkills string
	Active         bool
	CreatedBy      string
	CreatedAt      string
}

// AuditRecord is a single entry in the audit log.
type AuditRecord struct {
	ID        int64
	EventType string
	Detail    string
	Severity  string
	CreatedAt string
}

// PendingResponse is a response that failed delivery and is queued for retry.
type PendingResponse struct {
	ID         int64
	EventType  string
	ChatID     string
	Response   string
	RetryCount int
	CreatedAt  string
	NextRetry  string
}

// ---------------------------------------------------------------------------
// Store
// ---------------------------------------------------------------------------

// Store wraps a SQLite database providing all persistence for gopaw.
type Store struct {
	db *sql.DB
}

// Open creates or opens a SQLite database at path, enables WAL mode and
// foreign keys, then runs all pending migrations in order.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store open: %w", err)
	}

	// Pragmas: WAL for concurrency, foreign keys for integrity.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("store pragma %q: %w", pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("store migrate: %w", err)
	}
	return s, nil
}

// Close releases the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// ---------------------------------------------------------------------------
// Migrations
// ---------------------------------------------------------------------------

func (s *Store) migrate() error {
	// Ensure the migrations meta table exists.
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		filename TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return err
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	// Sort by filename to guarantee order.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}

		// Skip already-applied migrations.
		var count int
		if err := s.db.QueryRow(
			"SELECT COUNT(*) FROM _migrations WHERE filename = ?", name,
		).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}

		data, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		if _, err := s.db.Exec(string(data)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}

		if _, err := s.db.Exec(
			"INSERT INTO _migrations (filename) VALUES (?)", name,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", name, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Agent State
// ---------------------------------------------------------------------------

// SaveState upserts the agent row and replaces all conversation turns.
func (s *Store) SaveState(state *core.AgentState) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stateJSON, err := json.Marshal(state)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`
		INSERT INTO agents (agent_id, system_prompt, state_json, updated_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(agent_id) DO UPDATE SET
			system_prompt = excluded.system_prompt,
			state_json    = excluded.state_json,
			updated_at    = datetime('now')`,
		state.AgentID, state.SystemPrompt, string(stateJSON))
	if err != nil {
		return err
	}

	// Replace turns: delete existing, insert fresh.
	if _, err := tx.Exec("DELETE FROM conversations WHERE agent_id = ?", state.AgentID); err != nil {
		return err
	}

	stmt, err := tx.Prepare(`
		INSERT INTO conversations (agent_id, role, content, code, result, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range state.Turns {
		if _, err := stmt.Exec(
			state.AgentID,
			string(t.Role),
			t.Content,
			nullString(t.Code),
			nullString(t.Result),
			t.Timestamp,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoadState retrieves agent metadata and the most recent conversation turns.
func (s *Store) LoadState(agentID string) (*core.AgentState, error) {
	var sysPrompt string
	err := s.db.QueryRow(
		"SELECT system_prompt FROM agents WHERE agent_id = ?", agentID,
	).Scan(&sysPrompt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`
		SELECT role, content, code, result, timestamp
		FROM conversations
		WHERE agent_id = ?
		ORDER BY id DESC
		LIMIT ?`, agentID, core.MaxHistoryTurns)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var turns []core.ConversationTurn
	for rows.Next() {
		var t core.ConversationTurn
		var code, result sql.NullString
		if err := rows.Scan(&t.Role, &t.Content, &code, &result, &t.Timestamp); err != nil {
			return nil, err
		}
		t.Code = code.String
		t.Result = result.String
		turns = append(turns, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse to chronological order (query was DESC).
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}

	return &core.AgentState{
		AgentID:      agentID,
		SystemPrompt: sysPrompt,
		Turns:        turns,
	}, nil
}

// AddTurn appends a single turn to an agent's conversation.
// The agent row is upserted if it does not yet exist.
func (s *Store) AddTurn(agentID string, turn *core.ConversationTurn) error {
	// Ensure agent exists.
	if _, err := s.db.Exec(`
		INSERT INTO agents (agent_id) VALUES (?)
		ON CONFLICT(agent_id) DO UPDATE SET updated_at = datetime('now')`,
		agentID,
	); err != nil {
		return err
	}

	_, err := s.db.Exec(`
		INSERT INTO conversations (agent_id, role, content, code, result, timestamp)
		VALUES (?, ?, ?, ?, ?, ?)`,
		agentID,
		string(turn.Role),
		turn.Content,
		nullString(turn.Code),
		nullString(turn.Result),
		turn.Timestamp,
	)
	return err
}

// ListAgents returns all agents with their turn counts.
func (s *Store) ListAgents() ([]AgentSummary, error) {
	rows, err := s.db.Query(`
		SELECT a.agent_id, COUNT(c.id), a.created_at, a.updated_at
		FROM agents a
		LEFT JOIN conversations c ON c.agent_id = a.agent_id
		GROUP BY a.agent_id
		ORDER BY a.updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentSummary
	for rows.Next() {
		var a AgentSummary
		if err := rows.Scan(&a.AgentID, &a.TurnCount, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CountUserMessagesTotal returns the total number of user-role messages across
// all agents.
func (s *Store) CountUserMessagesTotal() (int, error) {
	var n int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM conversations WHERE role = 'user'",
	).Scan(&n)
	return n, err
}

// RecentUserMessagesAll returns user messages from the last `hours` hours,
// truncated so the combined length does not exceed maxChars.
func (s *Store) RecentUserMessagesAll(hours int, maxChars int) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT content FROM conversations
		WHERE role = 'user'
		  AND timestamp >= datetime('now', ?)
		ORDER BY timestamp DESC`,
		fmt.Sprintf("-%d hours", hours))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	total := 0
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		if total+len(c) > maxChars {
			break
		}
		total += len(c)
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Execution History
// ---------------------------------------------------------------------------

// RecordExecution inserts a new execution history record.
func (s *Store) RecordExecution(rec *ExecutionRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO execution_history
			(skill_id, skill_name, started_at, finished_at, duration_ms,
			 input_params, result_summary, success, retry_count, usage_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.SkillID, rec.SkillName, rec.StartedAt,
		nullString(rec.FinishedAt), rec.DurationMs,
		nullString(rec.InputParams), nullString(rec.ResultSummary),
		boolToInt(rec.Success), rec.RetryCount,
		nullString(rec.UsageJSON))
	return err
}

// RecentExecutions returns the most recent execution records.
func (s *Store) RecentExecutions(limit int) ([]ExecutionRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, skill_id, skill_name, started_at,
			   COALESCE(finished_at,''), COALESCE(duration_ms,0),
			   COALESCE(input_params,''), COALESCE(result_summary,''),
			   success, retry_count, COALESCE(usage_json,'')
		FROM execution_history
		ORDER BY started_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExecutions(rows)
}

// TodayStats returns aggregated execution statistics for the current day (UTC).
func (s *Store) TodayStats() (*ExecutionStats, error) {
	var st ExecutionStats
	err := s.db.QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(retry_count), 0)
		FROM execution_history
		WHERE started_at >= date('now')`).Scan(
		&st.TotalRuns, &st.Successful, &st.Failed, &st.AutoRetries)
	if err != nil {
		return nil, err
	}

	// Sum tokens from usage_json where available.
	var totalTokens sql.NullInt64
	err = s.db.QueryRow(`
		SELECT SUM(
			COALESCE(json_extract(usage_json, '$.total_tokens'), 0)
		)
		FROM execution_history
		WHERE started_at >= date('now')
		  AND usage_json IS NOT NULL`).Scan(&totalTokens)
	if err != nil {
		return nil, err
	}
	st.TotalTokens = totalTokens.Int64
	return &st, nil
}

// SearchExecutions performs a full-text search over execution history.
func (s *Store) SearchExecutions(query string, limit int) ([]ExecutionRecord, error) {
	rows, err := s.db.Query(`
		SELECT e.id, e.skill_id, e.skill_name, e.started_at,
			   COALESCE(e.finished_at,''), COALESCE(e.duration_ms,0),
			   COALESCE(e.input_params,''), COALESCE(e.result_summary,''),
			   e.success, e.retry_count, COALESCE(e.usage_json,'')
		FROM execution_history e
		JOIN execution_fts f ON f.rowid = e.id
		WHERE execution_fts MATCH ?
		ORDER BY e.started_at DESC
		LIMIT ?`, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExecutions(rows)
}

// SkillExecutionCount returns how many times a specific skill has been executed.
func (s *Store) SkillExecutionCount(skillID string) (int, error) {
	var n int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM execution_history WHERE skill_id = ?", skillID,
	).Scan(&n)
	return n, err
}

// CleanupOldExecutions removes execution records older than the given number of
// days and returns how many rows were deleted.
func (s *Store) CleanupOldExecutions(days int) (int, error) {
	res, err := s.db.Exec(`
		DELETE FROM execution_history
		WHERE started_at < datetime('now', ?)`,
		fmt.Sprintf("-%d days", days))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ---------------------------------------------------------------------------
// Storage (namespaced KV)
// ---------------------------------------------------------------------------

// StorageGet retrieves a value from namespaced key-value storage.
func (s *Store) StorageGet(namespace, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(
		"SELECT value FROM skill_storage WHERE namespace = ? AND key = ?",
		namespace, key,
	).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// StorageSet upserts a value in namespaced key-value storage.
func (s *Store) StorageSet(namespace, key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO skill_storage (namespace, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT(namespace, key) DO UPDATE SET value = excluded.value`,
		namespace, key, value)
	return err
}

// StorageDelete removes a key from namespaced storage.
func (s *Store) StorageDelete(namespace, key string) error {
	_, err := s.db.Exec(
		"DELETE FROM skill_storage WHERE namespace = ? AND key = ?",
		namespace, key)
	return err
}

// StorageList returns all keys in a namespace.
func (s *Store) StorageList(namespace string) ([]string, error) {
	rows, err := s.db.Query(
		"SELECT key FROM skill_storage WHERE namespace = ? ORDER BY key",
		namespace)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ---------------------------------------------------------------------------
// User Context
// ---------------------------------------------------------------------------

// SetUserContext upserts a user context key.
func (s *Store) SetUserContext(key, value, source string) error {
	_, err := s.db.Exec(`
		INSERT INTO user_context (key, value, source, updated_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET
			value      = excluded.value,
			source     = excluded.source,
			updated_at = datetime('now')`,
		key, value, source)
	return err
}

// GetUserContext retrieves a single user context value.
func (s *Store) GetUserContext(key string) (string, bool, error) {
	var v string
	err := s.db.QueryRow(
		"SELECT value FROM user_context WHERE key = ?", key,
	).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// ListUserContextPrefix returns all key-value pairs whose keys start with
// the given prefix.
func (s *Store) ListUserContextPrefix(prefix string) ([]KeyValue, error) {
	rows, err := s.db.Query(
		"SELECT key, value FROM user_context WHERE key LIKE ? ORDER BY key",
		prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []KeyValue
	for rows.Next() {
		var kv KeyValue
		if err := rows.Scan(&kv.Key, &kv.Value); err != nil {
			return nil, err
		}
		out = append(out, kv)
	}
	return out, rows.Err()
}

// DeleteUserContext removes a user context key. Returns true if a row was
// actually deleted.
func (s *Store) DeleteUserContext(key string) (bool, error) {
	res, err := s.db.Exec("DELETE FROM user_context WHERE key = ?", key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ---------------------------------------------------------------------------
// Cross-Channel Identity
// ---------------------------------------------------------------------------

// LinkIdentity associates a channel-specific user ID with a global user ID.
func (s *Store) LinkIdentity(globalUserID, channel, channelUserID string) error {
	_, err := s.db.Exec(`
		INSERT INTO user_identities (global_user_id, channel, channel_user_id)
		VALUES (?, ?, ?)
		ON CONFLICT(channel, channel_user_id) DO UPDATE SET
			global_user_id = excluded.global_user_id`,
		globalUserID, channel, channelUserID)
	return err
}

// ResolveUser looks up the global user ID for a given channel identity.
func (s *Store) ResolveUser(channel, channelUserID string) (string, bool, error) {
	var gid string
	err := s.db.QueryRow(
		"SELECT global_user_id FROM user_identities WHERE channel = ? AND channel_user_id = ?",
		channel, channelUserID,
	).Scan(&gid)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return gid, true, nil
}

// UnlinkIdentity removes a channel binding for a global user.
func (s *Store) UnlinkIdentity(globalUserID, channel string) error {
	_, err := s.db.Exec(
		"DELETE FROM user_identities WHERE global_user_id = ? AND channel = ?",
		globalUserID, channel)
	return err
}

// ListIdentities returns all channel identities for a global user.
func (s *Store) ListIdentities(globalUserID string) ([]UserIdentity, error) {
	rows, err := s.db.Query(`
		SELECT channel, channel_user_id, created_at
		FROM user_identities
		WHERE global_user_id = ?
		ORDER BY created_at`, globalUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []UserIdentity
	for rows.Next() {
		var u UserIdentity
		if err := rows.Scan(&u.Channel, &u.ChannelUserID, &u.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Checkpoints
// ---------------------------------------------------------------------------

// CreateCheckpoint saves a checkpoint at the current latest conversation row
// for an agent. Returns the new checkpoint ID.
func (s *Store) CreateCheckpoint(agentID, label string) (int64, error) {
	var maxID int64
	err := s.db.QueryRow(
		"SELECT COALESCE(MAX(id), 0) FROM conversations WHERE agent_id = ?",
		agentID,
	).Scan(&maxID)
	if err != nil {
		return 0, err
	}

	res, err := s.db.Exec(`
		INSERT INTO agent_checkpoints (agent_id, label, conv_row_id)
		VALUES (?, ?, ?)`, agentID, label, maxID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// RollbackToCheckpoint deletes all conversation rows after the checkpoint's
// saved row ID. Returns the number of deleted rows.
func (s *Store) RollbackToCheckpoint(checkpointID int64) (int, error) {
	var agentID string
	var convRowID int64
	err := s.db.QueryRow(
		"SELECT agent_id, conv_row_id FROM agent_checkpoints WHERE id = ?",
		checkpointID,
	).Scan(&agentID, &convRowID)
	if err != nil {
		return 0, err
	}

	res, err := s.db.Exec(
		"DELETE FROM conversations WHERE agent_id = ? AND id > ?",
		agentID, convRowID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ListCheckpoints returns all checkpoints for an agent.
func (s *Store) ListCheckpoints(agentID string) ([]Checkpoint, error) {
	rows, err := s.db.Query(`
		SELECT id, agent_id, label, conv_row_id, created_at
		FROM agent_checkpoints
		WHERE agent_id = ?
		ORDER BY id DESC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Checkpoint
	for rows.Next() {
		var c Checkpoint
		if err := rows.Scan(&c.ID, &c.AgentID, &c.Label, &c.ConvRowID, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Skill Fixes
// ---------------------------------------------------------------------------

// RecordFix stores a skill code correction.
func (s *Store) RecordFix(skillID, errorMsg, oldCode, newCode string) error {
	_, err := s.db.Exec(`
		INSERT INTO skill_fixes (skill_id, error_msg, old_code, new_code)
		VALUES (?, ?, ?, ?)`,
		skillID, errorMsg, oldCode, newCode)
	return err
}

// ListFixes returns all fixes for a skill, most recent first.
func (s *Store) ListFixes(skillID string) ([]SkillFix, error) {
	rows, err := s.db.Query(`
		SELECT id, skill_id, error_msg, old_code, new_code, applied, created_at
		FROM skill_fixes
		WHERE skill_id = ?
		ORDER BY created_at DESC`, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []SkillFix
	for rows.Next() {
		var f SkillFix
		var applied int
		if err := rows.Scan(&f.ID, &f.SkillID, &f.ErrorMsg, &f.OldCode, &f.NewCode, &applied, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.Applied = applied != 0
		out = append(out, f)
	}
	return out, rows.Err()
}

// ApplyFix marks a fix as applied. Returns true if the row existed and was
// not already applied.
func (s *Store) ApplyFix(fixID int64) (bool, error) {
	res, err := s.db.Exec(
		"UPDATE skill_fixes SET applied = 1 WHERE id = ? AND applied = 0",
		fixID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ---------------------------------------------------------------------------
// Permissions
// ---------------------------------------------------------------------------

// SaveFileRule upserts a file permission rule.
func (s *Store) SaveFileRule(rule *FilePermissionRule) error {
	_, err := s.db.Exec(`
		INSERT INTO permission_file_rules
			(id, workspace_id, path_pattern, is_exception, can_read, can_write, can_delete)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			workspace_id = excluded.workspace_id,
			path_pattern = excluded.path_pattern,
			is_exception = excluded.is_exception,
			can_read     = excluded.can_read,
			can_write    = excluded.can_write,
			can_delete   = excluded.can_delete`,
		rule.ID, rule.WorkspaceID, rule.PathPattern,
		boolToInt(rule.IsException),
		boolToInt(rule.CanRead),
		boolToInt(rule.CanWrite),
		boolToInt(rule.CanDelete))
	return err
}

// ListFileRules returns all file permission rules for a workspace.
func (s *Store) ListFileRules(workspaceID string) ([]FilePermissionRule, error) {
	rows, err := s.db.Query(`
		SELECT id, workspace_id, path_pattern, is_exception,
			   can_read, can_write, can_delete, created_at
		FROM permission_file_rules
		WHERE workspace_id = ?
		ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []FilePermissionRule
	for rows.Next() {
		var r FilePermissionRule
		var isExc, canR, canW, canD int
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.PathPattern,
			&isExc, &canR, &canW, &canD, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.IsException = isExc != 0
		r.CanRead = canR != 0
		r.CanWrite = canW != 0
		r.CanDelete = canD != 0
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteFileRule removes a file permission rule by ID.
func (s *Store) DeleteFileRule(ruleID string) error {
	_, err := s.db.Exec(
		"DELETE FROM permission_file_rules WHERE id = ?", ruleID)
	return err
}

// SaveNetworkRule upserts a network permission rule.
func (s *Store) SaveNetworkRule(rule *NetworkPermissionRule) error {
	_, err := s.db.Exec(`
		INSERT INTO permission_network_rules
			(id, workspace_id, domain_pattern, allowed_methods)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			workspace_id    = excluded.workspace_id,
			domain_pattern  = excluded.domain_pattern,
			allowed_methods = excluded.allowed_methods`,
		rule.ID, rule.WorkspaceID, rule.DomainPattern, rule.AllowedMethods)
	return err
}

// ListNetworkRules returns all network permission rules for a workspace.
func (s *Store) ListNetworkRules(workspaceID string) ([]NetworkPermissionRule, error) {
	rows, err := s.db.Query(`
		SELECT id, workspace_id, domain_pattern, allowed_methods, created_at
		FROM permission_network_rules
		WHERE workspace_id = ?
		ORDER BY created_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []NetworkPermissionRule
	for rows.Next() {
		var r NetworkPermissionRule
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.DomainPattern, &r.AllowedMethods, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SaveGlobalPath upserts a globally permitted filesystem path.
func (s *Store) SaveGlobalPath(path *GlobalPath) error {
	_, err := s.db.Exec(`
		INSERT INTO permission_global_paths (id, path, access_type)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			path        = excluded.path,
			access_type = excluded.access_type`,
		path.ID, path.Path, path.AccessType)
	return err
}

// ListGlobalPaths returns all globally permitted paths.
func (s *Store) ListGlobalPaths() ([]GlobalPath, error) {
	rows, err := s.db.Query(`
		SELECT id, path, access_type, created_at
		FROM permission_global_paths
		ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []GlobalPath
	for rows.Next() {
		var g GlobalPath
		if err := rows.Scan(&g.ID, &g.Path, &g.AccessType, &g.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// GrantCapability records a global capability grant.
func (s *Store) GrantCapability(capability string) error {
	_, err := s.db.Exec(`
		INSERT INTO global_grants (capability)
		VALUES (?)
		ON CONFLICT(capability) DO NOTHING`, capability)
	return err
}

// HasCapabilityGrant checks whether a capability has been granted.
func (s *Store) HasCapabilityGrant(capability string) (bool, error) {
	var n int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM global_grants WHERE capability = ?", capability,
	).Scan(&n)
	return n > 0, err
}

// RevokeCapability removes a global capability grant.
func (s *Store) RevokeCapability(capability string) error {
	_, err := s.db.Exec(
		"DELETE FROM global_grants WHERE capability = ?", capability)
	return err
}

// ---------------------------------------------------------------------------
// Profile Management
// ---------------------------------------------------------------------------

// UpsertProfileMeta creates or updates a profile's metadata.
func (s *Store) UpsertProfileMeta(id, description, equippedSkills, createdBy string) error {
	_, err := s.db.Exec(`
		INSERT INTO profile_meta (id, description, equipped_skills, created_by)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			description     = excluded.description,
			equipped_skills = excluded.equipped_skills,
			created_by      = excluded.created_by`,
		id, description, equippedSkills, createdBy)
	return err
}

// GetProfileMeta retrieves a single profile by ID.
func (s *Store) GetProfileMeta(id string) (*ProfileMeta, bool, error) {
	var p ProfileMeta
	var active int
	err := s.db.QueryRow(`
		SELECT id, description, equipped_skills, active, created_by, created_at
		FROM profile_meta WHERE id = ?`, id,
	).Scan(&p.ID, &p.Description, &p.EquippedSkills, &active, &p.CreatedBy, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	p.Active = active != 0
	return &p, true, nil
}

// ListActiveProfiles returns all profiles where active = 1.
func (s *Store) ListActiveProfiles() ([]ProfileMeta, error) {
	rows, err := s.db.Query(`
		SELECT id, description, equipped_skills, active, created_by, created_at
		FROM profile_meta
		WHERE active = 1
		ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ProfileMeta
	for rows.Next() {
		var p ProfileMeta
		var active int
		if err := rows.Scan(&p.ID, &p.Description, &p.EquippedSkills, &active, &p.CreatedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Active = active != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetProfileActive enables or disables a profile.
func (s *Store) SetProfileActive(id string, active bool) error {
	_, err := s.db.Exec(
		"UPDATE profile_meta SET active = ? WHERE id = ?",
		boolToInt(active), id)
	return err
}

// UpdateEquippedSkills replaces the equipped skills JSON for a profile.
func (s *Store) UpdateEquippedSkills(id, skills string) error {
	_, err := s.db.Exec(
		"UPDATE profile_meta SET equipped_skills = ? WHERE id = ?",
		skills, id)
	return err
}

// ---------------------------------------------------------------------------
// Scheduled Skills
// ---------------------------------------------------------------------------

// GetLastRun returns the last run time for a scheduled skill, or nil if never
// run.
func (s *Store) GetLastRun(skillName string) (*time.Time, error) {
	var raw sql.NullString
	err := s.db.QueryRow(
		"SELECT last_run_at FROM skill_schedule WHERE skill_name = ?",
		skillName,
	).Scan(&raw)
	if err == sql.ErrNoRows || !raw.Valid {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t, err := time.Parse(time.RFC3339, raw.String)
	if err != nil {
		// Fall back to SQLite datetime format.
		t, err = time.Parse("2006-01-02 15:04:05", raw.String)
		if err != nil {
			return nil, fmt.Errorf("parse last_run_at %q: %w", raw.String, err)
		}
	}
	return &t, nil
}

// SetLastRun records the last execution time for a scheduled skill.
func (s *Store) SetLastRun(skillName string, t time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO skill_schedule (skill_name, last_run_at)
		VALUES (?, ?)
		ON CONFLICT(skill_name) DO UPDATE SET last_run_at = excluded.last_run_at`,
		skillName, t.UTC().Format(time.RFC3339))
	return err
}

// GetFailureCount returns the consecutive failure count for a scheduled skill.
func (s *Store) GetFailureCount(skillName string) (int, error) {
	var n int
	err := s.db.QueryRow(
		"SELECT failure_count FROM skill_schedule WHERE skill_name = ?",
		skillName,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

// IncrementFailureCount increases the failure count by one, upserting the row
// if needed.
func (s *Store) IncrementFailureCount(skillName string) error {
	_, err := s.db.Exec(`
		INSERT INTO skill_schedule (skill_name, failure_count)
		VALUES (?, 1)
		ON CONFLICT(skill_name) DO UPDATE SET
			failure_count = skill_schedule.failure_count + 1`,
		skillName)
	return err
}

// ResetFailureCount sets the failure count back to zero.
func (s *Store) ResetFailureCount(skillName string) error {
	_, err := s.db.Exec(`
		UPDATE skill_schedule SET failure_count = 0
		WHERE skill_name = ?`, skillName)
	return err
}

// ---------------------------------------------------------------------------
// Audit
// ---------------------------------------------------------------------------

// RecordAudit appends an entry to the audit log.
func (s *Store) RecordAudit(eventType, detail, severity string) error {
	_, err := s.db.Exec(`
		INSERT INTO audit_log (event_type, detail, severity)
		VALUES (?, ?, ?)`, eventType, detail, severity)
	return err
}

// RecentAuditEvents returns the most recent audit log entries.
func (s *Store) RecentAuditEvents(limit int) ([]AuditRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, event_type, detail, severity, created_at
		FROM audit_log
		ORDER BY id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditRecord
	for rows.Next() {
		var a AuditRecord
		if err := rows.Scan(&a.ID, &a.EventType, &a.Detail, &a.Severity, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Pending Responses
// ---------------------------------------------------------------------------

const maxPendingRetries = 5

// EnqueueResponse saves a failed response for later retry.
func (s *Store) EnqueueResponse(eventType, chatID, response string) error {
	_, err := s.db.Exec(`
		INSERT INTO pending_responses (event_type, chat_id, response)
		VALUES (?, ?, ?)`, eventType, chatID, response)
	return err
}

// DequeuePendingResponses returns up to limit responses whose next_retry is in the past.
func (s *Store) DequeuePendingResponses(limit int) ([]PendingResponse, error) {
	rows, err := s.db.Query(`
		SELECT id, event_type, chat_id, response, retry_count, created_at, next_retry
		FROM pending_responses
		WHERE next_retry <= datetime('now')
		ORDER BY next_retry ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PendingResponse
	for rows.Next() {
		var p PendingResponse
		if err := rows.Scan(&p.ID, &p.EventType, &p.ChatID, &p.Response,
			&p.RetryCount, &p.CreatedAt, &p.NextRetry); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MarkResponseDelivered removes a successfully delivered pending response.
func (s *Store) MarkResponseDelivered(id int64) error {
	_, err := s.db.Exec(`DELETE FROM pending_responses WHERE id = ?`, id)
	return err
}

// IncrementResponseRetry bumps the retry count and sets exponential backoff.
// Returns false if max retries exceeded (row deleted).
// The SELECT + UPDATE/DELETE is wrapped in a transaction for atomicity.
func (s *Store) IncrementResponseRetry(id int64) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var count int
	err = tx.QueryRow(`SELECT retry_count FROM pending_responses WHERE id = ?`, id).Scan(&count)
	if err != nil {
		return false, err
	}
	count++
	if count >= maxPendingRetries {
		_, err := tx.Exec(`DELETE FROM pending_responses WHERE id = ?`, id)
		if err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	// Exponential backoff: 60s, 120s, 240s, 480s
	delaySec := 30 * (1 << count)
	_, err = tx.Exec(`
		UPDATE pending_responses
		SET retry_count = ?, next_retry = datetime('now', '+' || ? || ' seconds')
		WHERE id = ?`, count, delaySec, id)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// CleanupExpiredResponses deletes pending responses older than the given hours.
func (s *Store) CleanupExpiredResponses(hours int) (int, error) {
	result, err := s.db.Exec(`
		DELETE FROM pending_responses
		WHERE created_at < datetime('now', '-' || ? || ' hours')`, hours)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func scanExecutions(rows *sql.Rows) ([]ExecutionRecord, error) {
	var out []ExecutionRecord
	for rows.Next() {
		var r ExecutionRecord
		var success int
		if err := rows.Scan(
			&r.ID, &r.SkillID, &r.SkillName, &r.StartedAt,
			&r.FinishedAt, &r.DurationMs,
			&r.InputParams, &r.ResultSummary,
			&success, &r.RetryCount, &r.UsageJSON,
		); err != nil {
			return nil, err
		}
		r.Success = success != 0
		out = append(out, r)
	}
	return out, rows.Err()
}
