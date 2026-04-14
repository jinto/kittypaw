package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jinto/gopaw/core"
	_ "modernc.org/sqlite"
)

// WorkspaceFile is a file entry in the workspace index.
type WorkspaceFile struct {
	ID          int64
	WorkspaceID string
	AbsPath     string
	RelPath     string
	Filename    string
	Extension   string
	Size        int64
	ModifiedAt  string
	HasContent  bool
}

// WorkspaceFTSRow is a search result from the workspace FTS5 index.
type WorkspaceFTSRow struct {
	FileID    int64
	AbsPath   string
	RelPath   string
	Filename  string
	Extension string
	Size      int64
	Score     float64
	Snippet   string
}

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

// MemoryContextLines builds context sections for LLM prompt injection.
// Returns user facts, recent failures, and today's stats as markdown sections.
// Sections with no data are omitted entirely.
func (s *Store) MemoryContextLines() ([]string, error) {
	var sections []string

	// --- Remembered Facts (user_context, cap 20, most recent first) ---
	rows, err := s.db.Query(`
		SELECT key, value FROM user_context
		ORDER BY updated_at DESC
		LIMIT 20`)
	if err != nil {
		return nil, fmt.Errorf("memory context facts: %w", err)
	}
	var factLines []string
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return nil, fmt.Errorf("memory context scan fact: %w", err)
		}
		factLines = append(factLines, fmt.Sprintf("- %s: %s", sanitizeForPrompt(k, 100), sanitizeForPrompt(v, 500)))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory context facts iter: %w", err)
	}
	if len(factLines) > 0 {
		sections = append(sections, "### Remembered Facts\n"+strings.Join(factLines, "\n"))
	}

	// --- Recent Failures (last 24h UTC, cap 5) ---
	cutoff := time.Now().UTC().Add(-24 * time.Hour).Format("2006-01-02T15:04:05Z")
	rows, err = s.db.Query(`
		SELECT skill_name, COALESCE(result_summary, ''), started_at
		FROM execution_history
		WHERE success = 0
		  AND started_at >= ?
		ORDER BY started_at DESC
		LIMIT 5`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("memory context failures: %w", err)
	}
	defer rows.Close()

	var failLines []string
	for rows.Next() {
		var name, summary, ts string
		if err := rows.Scan(&name, &summary, &ts); err != nil {
			return nil, fmt.Errorf("memory context scan failure: %w", err)
		}
		failLines = append(failLines, fmt.Sprintf("- %s: %s (%s)", name, sanitizeForPrompt(summary, 200), ts))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("memory context failures iter: %w", err)
	}
	if len(failLines) > 0 {
		sections = append(sections, "### Recent Failures\n"+strings.Join(failLines, "\n"))
	}

	// --- Today's Stats ---
	stats, err := s.TodayStats()
	if err != nil {
		return nil, fmt.Errorf("memory context stats: %w", err)
	}
	if stats.TotalRuns > 0 {
		section := fmt.Sprintf(
			"### Today's Stats\n- Runs: %d (success: %d, failed: %d)\n- Retries: %d\n- Tokens used: %d",
			stats.TotalRuns, stats.Successful, stats.Failed, stats.AutoRetries, stats.TotalTokens,
		)
		sections = append(sections, section)
	}

	return sections, nil
}

// sanitizeForPrompt strips newlines and caps length to prevent prompt injection
// and token explosion from user-supplied or skill-generated content.
func sanitizeForPrompt(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
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
// Reflection / Evolution
// ---------------------------------------------------------------------------

// DeleteExpiredReflection removes user_context rows whose keys start with
// "reflection:" and whose updated_at is older than ttlDays days ago.
// Returns the number of deleted rows.
//
// Note: This performs a LIKE scan on user_context which is acceptable at
// current scale (<10K rows). If the table grows significantly, consider
// adding a partial index on the "reflection:" key prefix.
func (s *Store) DeleteExpiredReflection(ttlDays int) (int, error) {
	res, err := s.db.Exec(`
		DELETE FROM user_context
		WHERE key LIKE 'reflection:%'
		  AND updated_at <= datetime('now', ?)`,
		fmt.Sprintf("-%d days", ttlDays))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// DeleteUserContextPrefix removes all user_context rows matching a key prefix.
// Returns the number of deleted rows.
func (s *Store) DeleteUserContextPrefix(prefix string) (int, error) {
	res, err := s.db.Exec(
		"DELETE FROM user_context WHERE key LIKE ?", prefix+"%")
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
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

// RecordFix stores a skill code correction. When applied is true the fix is
// marked as already applied (auto-fix in full autonomy mode).
func (s *Store) RecordFix(skillID, errorMsg, oldCode, newCode string, applied bool) error {
	_, err := s.db.Exec(`
		INSERT INTO skill_fixes (skill_id, error_msg, old_code, new_code, applied)
		VALUES (?, ?, ?, ?, ?)`,
		skillID, errorMsg, oldCode, newCode, boolToInt(applied))
	return err
}

// GetFix returns a single fix by ID.
func (s *Store) GetFix(fixID int64) (*SkillFix, error) {
	var f SkillFix
	var applied int
	err := s.db.QueryRow(`
		SELECT id, skill_id, error_msg, old_code, new_code, applied, created_at
		FROM skill_fixes WHERE id = ?`, fixID,
	).Scan(&f.ID, &f.SkillID, &f.ErrorMsg, &f.OldCode, &f.NewCode, &applied, &f.CreatedAt)
	if err != nil {
		return nil, err
	}
	f.Applied = applied != 0
	return &f, nil
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

// ApplyFix marks a fix as applied after a stale-code check.
// It loads the fix row, compares old_code against currentCode (the code
// currently on disk), and rejects the fix if they differ.
// Returns (applied, error). Errors include ErrFixStale when the underlying
// code has changed since the fix was generated.
func (s *Store) ApplyFix(fixID int64, currentCode string) (bool, error) {
	var oldCode string
	var applied int
	err := s.db.QueryRow(
		"SELECT old_code, applied FROM skill_fixes WHERE id = ?", fixID,
	).Scan(&oldCode, &applied)
	if err != nil {
		return false, err
	}
	if applied != 0 {
		return false, nil // already applied
	}
	if oldCode != currentCode {
		return false, fmt.Errorf("fix %d is stale: code changed since fix was generated", fixID)
	}

	res, err := s.db.Exec(
		"UPDATE skill_fixes SET applied = 1 WHERE id = ? AND applied = 0",
		fixID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// RevertFix resets the applied flag back to 0. Used when the disk write
// fails after ApplyFix has already updated the DB, to keep DB/disk in sync.
func (s *Store) RevertFix(fixID int64) error {
	_, err := s.db.Exec("UPDATE skill_fixes SET applied = 0 WHERE id = ?", fixID)
	return err
}

// ---------------------------------------------------------------------------
// Workspaces
// ---------------------------------------------------------------------------

// Workspace represents a registered workspace directory.
type Workspace struct {
	ID           string
	Name         string
	RootPath     string
	CreatedAt    string
	LastOpenedAt string
}

// SaveWorkspace upserts a workspace. The root_path UNIQUE constraint prevents
// duplicate paths under different IDs.
func (s *Store) SaveWorkspace(ws *Workspace) error {
	_, err := s.db.Exec(`
		INSERT INTO workspaces (id, name, root_path)
		VALUES (?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name          = excluded.name,
			root_path     = excluded.root_path,
			last_opened_at = datetime('now')`,
		ws.ID, ws.Name, ws.RootPath)
	return err
}

// GetWorkspace returns a workspace by ID.
func (s *Store) GetWorkspace(id string) (*Workspace, error) {
	var ws Workspace
	err := s.db.QueryRow(`
		SELECT id, name, root_path, created_at, last_opened_at
		FROM workspaces WHERE id = ?`, id).
		Scan(&ws.ID, &ws.Name, &ws.RootPath, &ws.CreatedAt, &ws.LastOpenedAt)
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

// ListWorkspaces returns all registered workspaces ordered by creation time.
func (s *Store) ListWorkspaces() ([]Workspace, error) {
	rows, err := s.db.Query(`
		SELECT id, name, root_path, created_at, last_opened_at
		FROM workspaces ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Workspace
	for rows.Next() {
		var ws Workspace
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.RootPath, &ws.CreatedAt, &ws.LastOpenedAt); err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

// DeleteWorkspace removes a workspace by ID. Idempotent.
func (s *Store) DeleteWorkspace(id string) error {
	_, err := s.db.Exec("DELETE FROM workspaces WHERE id = ?", id)
	return err
}

// ListWorkspaceRootPaths returns just the root_path column for all workspaces.
// This is the hot path used by isPathAllowed.
func (s *Store) ListWorkspaceRootPaths() ([]string, error) {
	rows, err := s.db.Query("SELECT root_path FROM workspaces ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SeedWorkspacesFromConfig inserts TOML-configured paths into the workspaces
// table if they don't already exist. Paths are cleaned before insertion.
// Idempotent.
func (s *Store) SeedWorkspacesFromConfig(paths []string) error {
	ts := time.Now().UnixNano()
	for i, p := range paths {
		p = filepath.Clean(p)
		// Use root_path as a natural dedup key.
		var exists int
		s.db.QueryRow("SELECT COUNT(*) FROM workspaces WHERE root_path = ?", p).Scan(&exists)
		if exists > 0 {
			continue
		}
		id := fmt.Sprintf("ws-seed-%d-%d", ts, i)
		if _, err := s.db.Exec(
			"INSERT INTO workspaces (id, name, root_path) VALUES (?, ?, ?)",
			id, p, p,
		); err != nil {
			return err
		}
	}
	return nil
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

// GetFixAttempts returns the number of auto-fix attempts for a scheduled skill.
func (s *Store) GetFixAttempts(skillName string) (int, error) {
	var n int
	err := s.db.QueryRow(
		"SELECT fix_attempts FROM skill_schedule WHERE skill_name = ?",
		skillName,
	).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

// ClaimFixAttempt atomically increments fix_attempts only if still under the
// cap. Returns true if the claim succeeded (this goroutine should proceed with
// the fix). Uses UPDATE ... WHERE to prevent double-trigger races.
func (s *Store) ClaimFixAttempt(skillName string, maxAttempts int) (bool, error) {
	res, err := s.db.Exec(`
		UPDATE skill_schedule
		SET fix_attempts = fix_attempts + 1
		WHERE skill_name = ? AND fix_attempts < ?`,
		skillName, maxAttempts)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ResetFixAttempts sets the fix attempt counter back to zero (called when the
// skill succeeds after a fix).
func (s *Store) ResetFixAttempts(skillName string) error {
	_, err := s.db.Exec(`
		UPDATE skill_schedule SET fix_attempts = 0
		WHERE skill_name = ?`, skillName)
	return err
}

// UpdateFailureAndFixAttempts atomically increments failure_count and
// fix_attempts in a single statement to prevent crash-inconsistency.
func (s *Store) UpdateFailureAndFixAttempts(skillName string) error {
	_, err := s.db.Exec(`
		INSERT INTO skill_schedule (skill_name, failure_count, fix_attempts)
		VALUES (?, 1, 1)
		ON CONFLICT(skill_name) DO UPDATE SET
			failure_count = skill_schedule.failure_count + 1,
			fix_attempts = skill_schedule.fix_attempts + 1`,
		skillName)
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
// Permission Audit
// ---------------------------------------------------------------------------

// LogPermissionEvent records a permission decision to the audit log.
func (s *Store) LogPermissionEvent(decision, channel, chatID, description, resource string) error {
	detail, _ := json.Marshal(map[string]string{
		"channel":     channel,
		"chat_id":     chatID,
		"description": description,
		"resource":    resource,
		"decision":    decision,
	})
	return s.RecordAudit("permission."+decision, string(detail), "info")
}

// QueryPermissionLog returns recent permission audit entries.
func (s *Store) QueryPermissionLog(limit int) ([]AuditRecord, error) {
	rows, err := s.db.Query(`
		SELECT id, event_type, detail, severity, created_at
		FROM audit_log
		WHERE event_type LIKE 'permission.%'
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

// ---------------------------------------------------------------------------
// Workspace File Index (FTS5)
// ---------------------------------------------------------------------------

// UpsertWorkspaceFile inserts or replaces a file metadata entry. Returns the
// row ID for linking to the FTS5 index. The indexed_at timestamp is always
// refreshed so callers can use it for stale-entry cleanup after reindex.
func (s *Store) UpsertWorkspaceFile(f *WorkspaceFile) (int64, error) {
	hasContent := 0
	if f.HasContent {
		hasContent = 1
	}
	_, err := s.db.Exec(`
		INSERT INTO workspace_files (workspace_id, abs_path, rel_path, filename, extension, size, modified_at, has_content, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(workspace_id, abs_path) DO UPDATE SET
			rel_path     = excluded.rel_path,
			filename     = excluded.filename,
			extension    = excluded.extension,
			size         = excluded.size,
			modified_at  = excluded.modified_at,
			has_content  = excluded.has_content,
			indexed_at   = datetime('now')`,
		f.WorkspaceID, f.AbsPath, f.RelPath, f.Filename, f.Extension, f.Size, f.ModifiedAt, hasContent)
	if err != nil {
		return 0, err
	}
	// Always query the actual id — LastInsertId is unreliable for ON CONFLICT
	// DO UPDATE (SQLite may return a stale or auto-incremented phantom value).
	var id int64
	err = s.db.QueryRow(
		"SELECT id FROM workspace_files WHERE workspace_id = ? AND abs_path = ?",
		f.WorkspaceID, f.AbsPath,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// UpsertWorkspaceFTS replaces the FTS5 entry for a given file. For standalone
// FTS5 tables (no content= option), regular DELETE + INSERT is used.
func (s *Store) UpsertWorkspaceFTS(fileID int64, filename, body string) error {
	// Delete old entry if it exists. Standalone FTS5 supports regular DELETE.
	_, _ = s.db.Exec("DELETE FROM workspace_fts WHERE rowid = ?", fileID)
	_, err := s.db.Exec(
		"INSERT INTO workspace_fts(rowid, filename, body) VALUES(?, ?, ?)",
		fileID, filename, body)
	return err
}

// DeleteWorkspaceIndex removes all file metadata and FTS5 entries for a
// workspace atomically within a single transaction.
func (s *Store) DeleteWorkspaceIndex(wsID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		DELETE FROM workspace_fts
		WHERE rowid IN (SELECT id FROM workspace_files WHERE workspace_id = ?)`, wsID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM workspace_files WHERE workspace_id = ?", wsID); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteStaleWorkspaceFiles removes workspace_files entries (and their FTS5
// counterparts) whose indexed_at is older than the given cutoff string
// (format: "2006-01-02 15:04:05"). Runs atomically in a single transaction.
func (s *Store) DeleteStaleWorkspaceFiles(wsID string, cutoff string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`
		DELETE FROM workspace_fts
		WHERE rowid IN (
			SELECT id FROM workspace_files
			WHERE workspace_id = ? AND indexed_at < ?
		)`, wsID, cutoff); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"DELETE FROM workspace_files WHERE workspace_id = ? AND indexed_at < ?",
		wsID, cutoff); err != nil {
		return err
	}
	return tx.Commit()
}

// SearchWorkspaceFTS performs a full-text search across workspace files.
// Returns matching rows and the total count (independent of limit/offset).
// An empty query returns an error.
func (s *Store) SearchWorkspaceFTS(query, pathPrefix, ext string, limit, offset int) ([]WorkspaceFTSRow, int, error) {
	if query == "" {
		return nil, 0, fmt.Errorf("empty search query")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	// Sanitize query to prevent FTS5 syntax abuse.
	safeQuery := sanitizeFTSQuery(query)
	if safeQuery == "" {
		return nil, 0, fmt.Errorf("empty search query after sanitization")
	}

	// Build WHERE clauses.
	where := "WHERE workspace_fts MATCH ?"
	args := []any{safeQuery}
	if pathPrefix != "" {
		where += " AND wf.rel_path LIKE ? ESCAPE '\\'"
		args = append(args, escapeLIKE(pathPrefix)+"%")
	}
	if ext != "" {
		where += " AND wf.extension = ?"
		args = append(args, ext)
	}

	// Count total matches.
	countSQL := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM workspace_fts
		JOIN workspace_files wf ON wf.id = workspace_fts.rowid
		%s`, where)
	var total int
	if err := s.db.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("search count: %w", err)
	}

	// Fetch results — copy args to avoid mutating the original slice.
	searchSQL := fmt.Sprintf(`
		SELECT wf.id, wf.abs_path, wf.rel_path, wf.filename, wf.extension, wf.size,
		       rank,
		       snippet(workspace_fts, 1, '', '', '…', 64)
		FROM workspace_fts
		JOIN workspace_files wf ON wf.id = workspace_fts.rowid
		%s
		ORDER BY rank
		LIMIT ? OFFSET ?`, where)
	searchArgs := make([]any, len(args)+2)
	copy(searchArgs, args)
	searchArgs[len(args)] = limit
	searchArgs[len(args)+1] = offset

	rows, err := s.db.Query(searchSQL, searchArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var out []WorkspaceFTSRow
	for rows.Next() {
		var r WorkspaceFTSRow
		if err := rows.Scan(&r.FileID, &r.AbsPath, &r.RelPath, &r.Filename,
			&r.Extension, &r.Size, &r.Score, &r.Snippet); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// AggregateWorkspaceFiles returns statistics about indexed workspace files.
// If pathPrefix is non-empty, only files under that relative path are counted.
func (s *Store) AggregateWorkspaceFiles(pathPrefix string) (
	totalFiles, indexedFiles int,
	totalSize int64,
	byExt map[string][2]int64, // [count, size]
	latestAt string,
	err error,
) {
	byExt = make(map[string][2]int64)

	where := ""
	var args []any
	if pathPrefix != "" {
		where = " WHERE rel_path LIKE ? ESCAPE '\\'"
		args = append(args, escapeLIKE(pathPrefix)+"%")
	}

	// Totals.
	totalsSQL := fmt.Sprintf(`
		SELECT COUNT(*), COALESCE(SUM(CASE WHEN has_content = 1 THEN 1 ELSE 0 END), 0),
		       COALESCE(SUM(size), 0), COALESCE(MAX(indexed_at), '')
		FROM workspace_files%s`, where)
	if err = s.db.QueryRow(totalsSQL, args...).Scan(
		&totalFiles, &indexedFiles, &totalSize, &latestAt,
	); err != nil {
		return
	}

	// By extension.
	extSQL := fmt.Sprintf(`
		SELECT extension, COUNT(*), COALESCE(SUM(size), 0)
		FROM workspace_files%s
		GROUP BY extension
		ORDER BY COUNT(*) DESC`, where)
	rows, qErr := s.db.Query(extSQL, args...)
	if qErr != nil {
		err = qErr
		return
	}
	defer rows.Close()
	for rows.Next() {
		var ext string
		var cnt, sz int64
		if err = rows.Scan(&ext, &cnt, &sz); err != nil {
			return
		}
		byExt[ext] = [2]int64{cnt, sz}
	}
	err = rows.Err()
	return
}

// BeginTx starts a new database transaction. Used by the indexer for chunked
// batch inserts.
func (s *Store) BeginTx() (*sql.Tx, error) {
	return s.db.Begin()
}

// UpsertWorkspaceFileTx is the transactional variant of UpsertWorkspaceFile.
func (s *Store) UpsertWorkspaceFileTx(tx *sql.Tx, f *WorkspaceFile) (int64, error) {
	hasContent := 0
	if f.HasContent {
		hasContent = 1
	}
	_, err := tx.Exec(`
		INSERT INTO workspace_files (workspace_id, abs_path, rel_path, filename, extension, size, modified_at, has_content, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(workspace_id, abs_path) DO UPDATE SET
			rel_path     = excluded.rel_path,
			filename     = excluded.filename,
			extension    = excluded.extension,
			size         = excluded.size,
			modified_at  = excluded.modified_at,
			has_content  = excluded.has_content,
			indexed_at   = datetime('now')`,
		f.WorkspaceID, f.AbsPath, f.RelPath, f.Filename, f.Extension, f.Size, f.ModifiedAt, hasContent)
	if err != nil {
		return 0, err
	}
	var id int64
	err = tx.QueryRow(
		"SELECT id FROM workspace_files WHERE workspace_id = ? AND abs_path = ?",
		f.WorkspaceID, f.AbsPath,
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// UpsertWorkspaceFTSTx is the transactional variant of UpsertWorkspaceFTS.
func (s *Store) UpsertWorkspaceFTSTx(tx *sql.Tx, fileID int64, filename, body string) error {
	_, _ = tx.Exec("DELETE FROM workspace_fts WHERE rowid = ?", fileID)
	_, err := tx.Exec(
		"INSERT INTO workspace_fts(rowid, filename, body) VALUES(?, ?, ?)",
		fileID, filename, body)
	return err
}

// SQLiteNow returns the current time from SQLite's datetime('now') function.
// Used to ensure clock consistency with indexed_at timestamps.
func (s *Store) SQLiteNow() (string, error) {
	var now string
	err := s.db.QueryRow("SELECT datetime('now')").Scan(&now)
	return now, err
}

// sanitizeFTSQuery strips FTS5 special operators from a user-provided query,
// quoting each term as a literal to prevent query syntax abuse.
func sanitizeFTSQuery(query string) string {
	terms := strings.Fields(query)
	safe := make([]string, 0, len(terms))
	for _, t := range terms {
		// Strip FTS5 operators and special syntax characters.
		t = strings.Map(func(r rune) rune {
			switch r {
			case '"', '*', '^', '{', '}', ':', '(', ')':
				return -1
			}
			return r
		}, t)
		t = strings.TrimSpace(t)
		// Skip FTS5 boolean keywords.
		upper := strings.ToUpper(t)
		if t == "" || upper == "AND" || upper == "OR" || upper == "NOT" || upper == "NEAR" {
			continue
		}
		safe = append(safe, `"`+t+`"`)
	}
	return strings.Join(safe, " ")
}

// escapeLIKE escapes SQL LIKE wildcards (%, _) in a string.
// Use with ESCAPE '\' in the SQL clause.
func escapeLIKE(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
