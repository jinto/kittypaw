// Command seed-seoul-metro imports station coordinates from the
// Seoul Metro Lines 1-8 CSV (data.go.kr 15099316).
//
// Usage:
//
//	make seed-seoul-metro                                          # default csv path
//	go run ./cmd/seed-seoul-metro --csv=path/to/seoul_metro.csv    # explicit
//
// Download (manual):
//  1. Visit https://www.data.go.kr/data/15099316/fileData.do
//  2. Click Download (data.go.kr login required)
//  3. Save UTF-8 CSV to testdata/seoul_metro.csv
//
// Expected CSV columns:
//
//	연번, 호선, 역명, 구분, 위도, 경도
//
// On conflict (source, source_ref) the row is updated — safe to re-run.
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	csvPath := flag.String("csv", "testdata/seoul_metro.csv", "CSV file path")
	flag.Parse()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	f, err := os.Open(*csvPath)
	if err != nil {
		log.Fatalf("open csv %s: %v", *csvPath, err)
	}
	defer func() { _ = f.Close() }()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1 // tolerate variable column counts (we validate per row)

	// Resolve column indices by Korean header name. data.go.kr has reordered
	// columns on previous datasets, so positional indexing is fragile
	// (Adversarial finding #7). Fail loud if any required column is missing.
	header, err := r.Read()
	if err != nil {
		log.Fatalf("read header: %v", err)
	}
	idxLine, idxName, idxLat, idxLon := -1, -1, -1, -1
	assign := func(target *int, name string, i int) {
		if *target != -1 {
			log.Fatalf("CSV header has duplicate column %q (positions %d and %d) — refusing to import: %v", name, *target, i, header)
		}
		*target = i
	}
	for i, h := range header {
		switch strings.TrimSpace(h) {
		case "호선":
			assign(&idxLine, "호선", i)
		case "역명":
			assign(&idxName, "역명", i)
		case "위도":
			assign(&idxLat, "위도", i)
		case "경도":
			assign(&idxLon, "경도", i)
		}
	}
	if idxLine < 0 || idxName < 0 || idxLat < 0 || idxLon < 0 {
		log.Fatalf("CSV header missing required columns (need 호선, 역명, 위도, 경도): %v", header)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	total, skipped := 0, 0
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("csv read error: %v", err)
			skipped++
			continue
		}
		maxIdx := max(idxLine, idxName, idxLat, idxLon)
		if len(rec) <= maxIdx {
			log.Printf("skip short row (%d cols, need %d): %v", len(rec), maxIdx+1, rec)
			skipped++
			continue
		}
		line := rec[idxLine]
		name := rec[idxName]
		lat, errLat := strconv.ParseFloat(rec[idxLat], 64)
		lon, errLon := strconv.ParseFloat(rec[idxLon], 64)
		if errLat != nil || errLon != nil {
			log.Printf("skip bad coord: %v", rec)
			skipped++
			continue
		}

		sourceRef := fmt.Sprintf("seoul-metro:%s:%s", line, name)
		// Aliases: include line-qualified form so "강남역(2호선)" also matches.
		aliases := []string{name + "(" + line + ")"}

		_, err = pool.Exec(ctx, `
			INSERT INTO places (name_ko, aliases, lat, lon, type, source, source_ref, region, source_priority)
			VALUES ($1, $2, $3, $4, 'subway_station', 'kogl_seoul_metro', $5, '서울특별시', 30)
			ON CONFLICT (source, source_ref) DO UPDATE SET
				name_ko = EXCLUDED.name_ko,
				aliases = EXCLUDED.aliases,
				lat = EXCLUDED.lat,
				lon = EXCLUDED.lon,
				updated_at = now()
		`, name, aliases, lat, lon, sourceRef)
		if err != nil {
			log.Printf("upsert %s: %v", name, err)
			skipped++
			continue
		}
		total++
	}

	log.Printf("seed-seoul-metro complete: %d rows imported, %d skipped", total, skipped)
}
