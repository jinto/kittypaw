// Command seed-wikidata imports Korean landmark coordinates from Wikidata
// SPARQL into the places table. Source license: CC0 1.0 (no attribution
// required). Used by the /v1/geo/resolve handler as the landmark layer.
//
// Usage:
//
//	make seed-wikidata             # full reimport with transactional swap
//	go run ./cmd/seed-wikidata --resume  # resume from checkpoint after failure
//
// Required env: DATABASE_URL.
//
// Implementation notes:
//   - Stages rows into places_import_<run_id>, then transactional swap into
//     places (DELETE source='wikidata' → INSERT FROM staging → DROP staging).
//     A failure during paging leaves places untouched.
//   - Page size 1000, max retry 3 with exponential backoff.
//   - Spatial filter via wikibase:box (Korea bounding box: 33-39N, 124-132E).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	wikidataEndpoint = "https://query.wikidata.org/sparql"
	pageSize         = 1000
	maxRetry         = 3
	userAgent        = "kittypaw-api seed-wikidata/1.0 (https://github.com/kittypaw-app)"
)

// runIDRe validates the run_id token before it is interpolated into staging
// table SQL. Format: time.Format("20060102T150405Z") — exactly 16 chars,
// digits + 'T' + 'Z'. Resume reads run_id from a JSON checkpoint file which
// is untrusted (filesystem permissions, accidental edit, supply-chain).
var runIDRe = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z$`)

// SPARQL: all Wikidata items inside the Korea bbox with a Korean rdfs:label
// and (optionally) Korean/English skos:altLabel concatenated.
//
// Cursor-based paging via STR(?item) > %q rather than OFFSET — OFFSET is
// non-deterministic across page boundaries when items are added/removed
// between fetches (Adversarial finding #4). The cursor is the last QID URI
// from the previous page; checkpoints persist it for --resume.
const sparqlTemplate = `SELECT ?item ?label ?coord (GROUP_CONCAT(DISTINCT ?alt; SEPARATOR="\u001F") AS ?altLabels) WHERE {
  SERVICE wikibase:box {
    ?item wdt:P625 ?coord .
    bd:serviceParam wikibase:cornerSouthWest "Point(124 33)"^^geo:wktLiteral .
    bd:serviceParam wikibase:cornerNorthEast "Point(132 39)"^^geo:wktLiteral .
  }
  ?item rdfs:label ?label . FILTER(LANG(?label) = "ko") .
  FILTER(STR(?item) > "%s") .
  OPTIONAL { ?item skos:altLabel ?alt . FILTER(LANG(?alt) = "ko" || LANG(?alt) = "en") }
}
GROUP BY ?item ?label ?coord
ORDER BY ?item
LIMIT %d`

// altLabelSep separates aliases inside the SPARQL GROUP_CONCAT. ASCII Unit
// Separator (U+001F) is virtually never present in legitimate place names,
// avoiding the collision risk of a printable character like "|".
const altLabelSep = "\x1f"

type sparqlResponse struct {
	Results struct {
		Bindings []struct {
			Item      sparqlValue `json:"item"`
			Label     sparqlValue `json:"label"`
			Coord     sparqlValue `json:"coord"`
			AltLabels sparqlValue `json:"altLabels"`
		} `json:"bindings"`
	} `json:"results"`
}

type sparqlValue struct {
	Value string `json:"value"`
}

var coordRe = regexp.MustCompile(`Point\(([-0-9.]+) ([-0-9.]+)\)`)

// sparqlEscaper escapes backslashes, double quotes, and control characters
// for safe interpolation into a SPARQL string literal. SPARQL 1.1 forbids raw
// LF / CR / TAB inside `"..."` — they must be escape sequences. Constructed
// once at package init.
var sparqlEscaper = strings.NewReplacer(
	`\`, `\\`,
	`"`, `\"`,
	"\n", `\n`,
	"\r", `\r`,
	"\t", `\t`,
)

func main() {
	var (
		checkpointPath = flag.String("checkpoint", "places_import_state.json", "checkpoint file")
		resume         = flag.Bool("resume", false, "resume from checkpoint")
	)
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	cursor := "" // STR(?item) cursor; empty = start from the very beginning
	runID := time.Now().UTC().Format("20060102T150405Z")
	if *resume {
		if cp, err := loadCheckpoint(*checkpointPath); err == nil {
			if !runIDRe.MatchString(cp.RunID) {
				log.Fatalf("checkpoint run_id %q does not match expected format %s — refusing to interpolate into SQL identifier", cp.RunID, runIDRe.String())
			}
			cursor = cp.LastItem
			runID = cp.RunID
			log.Printf("resuming run %s from cursor %q", cp.RunID, cursor)
		} else {
			log.Printf("no checkpoint found, starting fresh: %v", err)
		}
	}

	// runID is now guaranteed safe by the regex; identifier sanitization is
	// a belt-and-suspenders second defense. Format: places_import_YYYYMMDDTHHMMSSZ.
	stagingTable := pgx.Identifier{"places_import_" + runID}.Sanitize()
	if !*resume {
		// Sweep stale staging tables left behind by SIGKILL/OOM in past runs
		// (Adversarial finding #1, Security finding #5). Any table older than
		// 24h is unlikely to belong to an in-flight run.
		cleanupStaleStagingTables(ctx, pool)
		if _, err := pool.Exec(ctx, fmt.Sprintf(
			"CREATE TABLE %s (LIKE places INCLUDING ALL)", stagingTable,
		)); err != nil {
			log.Fatalf("create staging table %s: %v", stagingTable, err)
		}
	} else {
		// On resume, verify the staging table still exists. A previous run
		// may have completed swap+drop but died before removing the
		// checkpoint file; resuming would otherwise insert into a missing
		// table and fail late.
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS (SELECT 1 FROM pg_tables WHERE tablename = $1)",
			"places_import_"+runID,
		).Scan(&exists)
		if err != nil {
			log.Fatalf("check staging table existence: %v", err)
		}
		if !exists {
			log.Printf("staging table for run %s no longer exists — previous run likely completed; clearing stale checkpoint", runID)
			_ = os.Remove(*checkpointPath)
			return
		}
	}

	client := &http.Client{Timeout: 90 * time.Second}
	total := 0
	for {
		rows, err := fetchPage(ctx, client, cursor, pageSize)
		if err != nil {
			log.Fatalf("fetch cursor=%q: %v", cursor, err)
		}
		if len(rows) == 0 {
			break
		}
		if err := insertRows(ctx, pool, stagingTable, rows); err != nil {
			log.Fatalf("insert cursor=%q: %v", cursor, err)
		}
		total += len(rows)
		// Last QID URI becomes the next cursor; results are sorted by ?item.
		cursor = rows[len(rows)-1].ItemURI
		log.Printf("imported count=%d total=%d cursor→%q", len(rows), total, cursor)
		saveCheckpoint(*checkpointPath, checkpoint{RunID: runID, LastItem: cursor, Total: total})
		if len(rows) < pageSize {
			break
		}
	}

	// Transactional swap: in a single tx, replace all wikidata rows with the
	// staging contents, then drop the staging table.
	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatalf("begin tx: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "DELETE FROM places WHERE source = 'wikidata'"); err != nil {
		log.Fatalf("delete wikidata: %v", err)
	}
	// Explicit column list (omit `id`) so the destination's BIGSERIAL
	// sequence advances naturally. Copying SELECT * would carry the
	// staging table's own sequence values and cause PK collisions on
	// subsequent manual INSERTs (Adversarial finding #3).
	if _, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO places
			(name_ko, aliases, lat, lon, type, source, source_ref, region,
			 source_priority, imported_at, updated_at)
		SELECT name_ko, aliases, lat, lon, type, source, source_ref, region,
			   source_priority, imported_at, updated_at
		FROM %s
	`, stagingTable)); err != nil {
		log.Fatalf("copy from staging: %v", err)
	}
	if _, err := tx.Exec(ctx, "DROP TABLE "+stagingTable); err != nil {
		log.Fatalf("drop staging: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatalf("commit: %v", err)
	}

	_ = os.Remove(*checkpointPath)
	log.Printf("seed-wikidata complete: %d rows", total)
}

type wikidataRow struct {
	QID     string // bare QID (e.g., "Q485389")
	ItemURI string // full URI for cursor (e.g., "http://www.wikidata.org/entity/Q485389")
	Label   string
	Lat     float64
	Lon     float64
	Aliases []string
}

func fetchPage(ctx context.Context, client *http.Client, cursor string, limit int) ([]wikidataRow, error) {
	// SPARQL string literal: backslash-escape any embedded quotes/backslashes.
	query := fmt.Sprintf(sparqlTemplate, sparqlEscaper.Replace(cursor), limit)
	var lastErr error
	for attempt := 1; attempt <= maxRetry; attempt++ {
		rows, err := doFetch(ctx, client, query)
		if err == nil {
			return rows, nil
		}
		lastErr = err
		backoff := time.Duration(1<<attempt) * time.Second
		log.Printf("fetch attempt %d failed: %v (retry in %v)", attempt, err, backoff)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
	return nil, fmt.Errorf("max retry exceeded: %w", lastErr)
}

func doFetch(ctx context.Context, client *http.Client, query string) ([]wikidataRow, error) {
	u, _ := url.Parse(wikidataEndpoint)
	q := u.Query()
	q.Set("query", query)
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := client.Do(req)
	if err != nil {
		// Sanitize: do not %w wrap — *url.Error stringifies the full URL.
		return nil, fmt.Errorf("sparql request failed")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("sparql status %d: %s", resp.StatusCode, body)
	}

	// Cap response body — defense against a misbehaving endpoint or redirect
	// streaming an unbounded payload (Adversarial finding #2). 64 MB is far
	// above expected page size (~few MB for 1000 results).
	const maxResponseBytes = 64 << 20
	var sr sparqlResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode sparql json: %w", err)
	}

	rows := make([]wikidataRow, 0, len(sr.Results.Bindings))
	for _, b := range sr.Results.Bindings {
		m := coordRe.FindStringSubmatch(b.Coord.Value)
		if len(m) != 3 {
			continue
		}
		lon, _ := strconv.ParseFloat(m[1], 64)
		lat, _ := strconv.ParseFloat(m[2], 64)
		itemURI := b.Item.Value
		qid := itemURI
		if i := strings.LastIndex(qid, "/"); i >= 0 {
			qid = qid[i+1:]
		}
		var aliases []string
		if b.AltLabels.Value != "" {
			for _, a := range strings.Split(b.AltLabels.Value, altLabelSep) {
				if s := strings.TrimSpace(a); s != "" {
					aliases = append(aliases, s)
				}
			}
		}
		rows = append(rows, wikidataRow{
			QID:     qid,
			ItemURI: itemURI,
			Label:   b.Label.Value,
			Lat:     lat,
			Lon:     lon,
			Aliases: aliases,
		})
	}
	return rows, nil
}

func insertRows(ctx context.Context, pool *pgxpool.Pool, table string, rows []wikidataRow) error {
	if len(rows) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	q := fmt.Sprintf(`INSERT INTO %s
		(name_ko, aliases, lat, lon, type, source, source_ref, source_priority)
		VALUES ($1, $2, $3, $4, 'landmark', 'wikidata', $5, 20)
		ON CONFLICT (source, source_ref) DO NOTHING`, table)
	for _, r := range rows {
		if r.Aliases == nil {
			r.Aliases = []string{}
		}
		batch.Queue(q, r.Label, r.Aliases, r.Lat, r.Lon, r.QID)
	}
	br := pool.SendBatch(ctx, batch)
	defer func() { _ = br.Close() }()
	for range rows {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

type checkpoint struct {
	RunID    string `json:"run_id"`
	LastItem string `json:"last_item"` // STR(?item) cursor — full Wikidata URI
	Total    int    `json:"total"`
}

func saveCheckpoint(path string, cp checkpoint) {
	b, err := json.Marshal(cp)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, b, 0o600)
}

// cleanupStaleStagingTables drops any places_import_* tables whose name
// timestamp is more than 24 hours old. Best-effort — logs and continues
// on error rather than failing the import.
func cleanupStaleStagingTables(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx,
		`SELECT tablename FROM pg_tables WHERE tablename LIKE 'places_import_%'`,
	)
	if err != nil {
		log.Printf("cleanup: enumerate stale staging tables: %v", err)
		return
	}
	defer rows.Close()

	cutoff := time.Now().UTC().Add(-24 * time.Hour)
	var stale []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		const prefix = "places_import_"
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		stamp := strings.TrimPrefix(name, prefix)
		t, err := time.Parse("20060102T150405Z", stamp)
		if err != nil || !t.Before(cutoff) {
			continue
		}
		stale = append(stale, name)
	}
	for _, name := range stale {
		// Validate via Identifier to avoid SQL injection on the (already
		// trusted) table name — defense-in-depth.
		ident := pgx.Identifier{name}.Sanitize()
		if _, err := pool.Exec(ctx, "DROP TABLE IF EXISTS "+ident); err != nil {
			log.Printf("cleanup: drop %s: %v", name, err)
			continue
		}
		log.Printf("cleanup: dropped stale staging table %s", name)
	}
}

func loadCheckpoint(path string) (checkpoint, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return checkpoint{}, err
	}
	var cp checkpoint
	err = json.Unmarshal(b, &cp)
	return cp, err
}
