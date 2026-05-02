package model

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/text/unicode/norm"
)

// Source priority constants — used by SQL ORDER BY tiebreaker. Higher wins.
const (
	SourceSeoulMetro    = "kogl_seoul_metro"
	SourceWikidata      = "wikidata"
	SourceKittypawAlias = "kittypaw_alias"

	PriorityKittypawAlias = 40
	PrioritySeoulMetro    = 30
	PriorityWikidata      = 20
)

// Place type constants.
const (
	TypeSubwayStation = "subway_station"
	TypeLandmark      = "landmark"
	TypeAliasOverride = "alias_override"
)

// MaxQueryLength caps the rune count of resolve query strings after NFC
// normalization. Inputs longer than this trigger HTTP 414. The handler
// also enforces a 6× byte cap on the raw input as a NFC-expansion defense.
const MaxQueryLength = 200

type Place struct {
	ID             int64
	NameKo         string
	Aliases        []string
	Lat            float64
	Lon            float64
	Type           string
	Source         string
	SourceRef      string
	Region         string
	SourcePriority int
	ImportedAt     time.Time
	UpdatedAt      time.Time
}

// AliasOverride is a hand-curated alias → coord mapping. Stored separately
// from places so that import cron jobs (Wikidata, Seoul Metro) cannot
// overwrite curator decisions.
type AliasOverride struct {
	ID         int64
	Alias      string
	TargetLat  float64
	TargetLon  float64
	TargetName string
	Note       string
	CreatedAt  time.Time
}

type PlaceStore interface {
	FindExact(ctx context.Context, name, typeHint string) (*Place, error)
	FindByAlias(ctx context.Context, alias, typeHint string) (*Place, error)
	FindByFuzzy(ctx context.Context, q, typeHint string, threshold float64) (*Place, error)
	FindAliasOverride(ctx context.Context, alias string) (*Place, error)
	Upsert(ctx context.Context, p *Place) error
}

type PostgresPlaceStore struct {
	pool *pgxpool.Pool
}

func NewPlaceStore(pool *pgxpool.Pool) *PostgresPlaceStore {
	return &PostgresPlaceStore{pool: pool}
}

// orderByClause is shared across exact/alias/fuzzy queries to keep result
// ordering deterministic.
//
// First key: typeHint match — when caller passes a hint ("subway_station"),
// rows with matching type sort first, but rows with other types still appear
// (downgraded). This avoids strict-filter starvation: e.g., "강남역" gets
// typeHint='subway_station', but Wikidata seeds the row as type='landmark';
// strict filter would miss it. Hint guides ordering, not membership.
//
// Second key: default type priority (landmark > subway_station). Used when no
// hint is given — natural-language input skews toward landmarks.
//
// Third/fourth: source_priority DESC, id ASC for full determinism.
const orderByClause = `
	(CASE WHEN $2::text = '' OR type = $2 THEN 0 ELSE 1 END) ASC,
	(CASE type WHEN 'landmark' THEN 0 WHEN 'subway_station' THEN 1 ELSE 2 END) ASC,
	source_priority DESC,
	id ASC
`

const placeColumns = `id, name_ko, aliases, lat, lon, type, source,
	COALESCE(source_ref, ''), COALESCE(region, ''), source_priority, imported_at, updated_at`

// FindExact returns a place whose name_ko equals name (NFC normalized).
// typeHint guides ordering (hint-matching rows first) but does not filter:
// callers with mismatched hints still get a result if one exists.
func (s *PostgresPlaceStore) FindExact(ctx context.Context, name, typeHint string) (*Place, error) {
	query := `SELECT ` + placeColumns + ` FROM places
		WHERE name_ko = $1
		ORDER BY ` + orderByClause + `LIMIT 1`
	return s.queryOne(ctx, query, name, typeHint)
}

// FindByAlias returns a place whose aliases array contains alias.
func (s *PostgresPlaceStore) FindByAlias(ctx context.Context, alias, typeHint string) (*Place, error) {
	query := `SELECT ` + placeColumns + ` FROM places
		WHERE $1 = ANY(aliases)
		ORDER BY ` + orderByClause + `LIMIT 1`
	return s.queryOne(ctx, query, alias, typeHint)
}

// FindByFuzzy returns the best place whose name_ko trigram-similarity to q
// exceeds threshold. ORDER BY is fully deterministic — same q must yield the
// same result every run.
func (s *PostgresPlaceStore) FindByFuzzy(ctx context.Context, q, typeHint string, threshold float64) (*Place, error) {
	query := `SELECT ` + placeColumns + ` FROM places
		WHERE similarity(name_ko, $1) > $3
		ORDER BY similarity(name_ko, $1) DESC, ` + orderByClause + `LIMIT 1`
	return s.queryOne(ctx, query, q, typeHint, threshold)
}

// FindAliasOverride looks up the curated alias_overrides table. Returns a
// synthetic Place (type='alias_override', source='kittypaw_alias') so the
// resolve handler can treat all matches uniformly.
func (s *PostgresPlaceStore) FindAliasOverride(ctx context.Context, alias string) (*Place, error) {
	var ao AliasOverride
	err := s.pool.QueryRow(ctx, `
		SELECT id, alias, target_lat, target_lon, target_name, COALESCE(note, ''), created_at
		FROM alias_overrides WHERE alias = $1
	`, alias).Scan(&ao.ID, &ao.Alias, &ao.TargetLat, &ao.TargetLon, &ao.TargetName, &ao.Note, &ao.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &Place{
		ID:             ao.ID,
		NameKo:         ao.TargetName,
		Lat:            ao.TargetLat,
		Lon:            ao.TargetLon,
		Type:           TypeAliasOverride,
		Source:         SourceKittypawAlias,
		SourcePriority: PriorityKittypawAlias,
	}, nil
}

// Upsert inserts or updates a place keyed by (source, source_ref).
// Used by seed-wikidata and seed-seoul-metro import cmds.
//
// `aliases` defaults to an empty array when nil — pgx encodes a Go nil
// slice as SQL NULL, which violates the column's NOT NULL constraint
// (caught by integration test before first prod seed).
func (s *PostgresPlaceStore) Upsert(ctx context.Context, p *Place) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO places (name_ko, aliases, lat, lon, type, source, source_ref, region, source_priority)
		VALUES ($1, COALESCE($2, ARRAY[]::text[]), $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), $9)
		ON CONFLICT (source, source_ref) DO UPDATE SET
			name_ko = EXCLUDED.name_ko,
			aliases = EXCLUDED.aliases,
			lat = EXCLUDED.lat,
			lon = EXCLUDED.lon,
			type = EXCLUDED.type,
			region = EXCLUDED.region,
			source_priority = EXCLUDED.source_priority,
			updated_at = now()
	`, p.NameKo, p.Aliases, p.Lat, p.Lon, p.Type, p.Source, p.SourceRef, p.Region, p.SourcePriority)
	return err
}

func (s *PostgresPlaceStore) queryOne(ctx context.Context, query string, args ...any) (*Place, error) {
	var p Place
	err := s.pool.QueryRow(ctx, query, args...).Scan(
		&p.ID, &p.NameKo, &p.Aliases, &p.Lat, &p.Lon, &p.Type, &p.Source,
		&p.SourceRef, &p.Region, &p.SourcePriority, &p.ImportedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &p, nil
}

// NormalizeQuery canonicalizes resolve input: NFC + trim + collapse internal
// whitespace runs to a single space. Mac clipboards often produce NFD-form
// Hangul (decomposed jamo) which doesn't match NFC strings stored in DB.
func NormalizeQuery(s string) string {
	s = norm.NFC.String(s)
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' {
			if !prevSpace {
				b.WriteByte(' ')
			}
			prevSpace = true
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}
