package proxy

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kittypaw-app/kittyapi/internal/model"
)

// FuzzyThreshold for pg_trgm similarity. v8 default — tune via corpus
// benchmark (testdata/korean_corpus.json) before lowering.
const FuzzyThreshold = 0.7

var (
	// `*역$` — Korean subway station naming convention. Drives the
	// `subway_station` type hint when input ends with "역".
	reSubwayStation = regexp.MustCompile(`역\s*$`)

	// `*로` or `*길` followed by a number — road address pattern. Reserved
	// for PR-2 (행안부 도로명주소 fallthrough).
	reAddress = regexp.MustCompile(`(로|길)\s+\d`)
)

// PlacesHandler resolves a free-form Korean location query to coordinates.
type PlacesHandler struct {
	Store model.PlaceStore
}

// detectTypeHint returns "subway_station", "address", or "" (unknown). The
// handler uses "" when no hint is available — the SQL ORDER BY then falls
// back to the type-priority CASE clause (landmark > subway_station).
func detectTypeHint(q string) string {
	if reSubwayStation.MatchString(q) {
		return model.TypeSubwayStation
	}
	if reAddress.MatchString(q) {
		return "address" // PR-2 reserved — handler ignores until addresses table exists
	}
	return ""
}

// Resolve handles GET /v1/geo/resolve?q={query}. Matching priority:
//
//  1. alias_overrides exact (curator override; defeats Wikidata mistakes)
//  2. places exact name_ko match
//  3. places aliases array contains q
//  4. places trigram fuzzy (similarity > FuzzyThreshold)
//  5. 422 unsupported_input
//
// PR-2 will insert an addresses-table fallthrough between (4) and (5).
func (h *PlacesHandler) Resolve() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			writeError(w, http.StatusBadRequest, ErrCodeMissingQ, nil)
			return
		}
		// Reject NUL bytes early — PostgreSQL TEXT columns reject them and
		// would surface as 500 internal_error rather than a clear 400.
		if strings.ContainsRune(q, 0) {
			writeError(w, http.StatusBadRequest, ErrCodeMissingQ, nil)
			return
		}
		// Hard byte cap defends against pathological NFC expansion (rare but
		// possible with combining marks). Roughly 6× the rune cap covers any
		// realistic Korean text.
		if len(q) > model.MaxQueryLength*6 {
			writeError(w, http.StatusRequestURITooLong, ErrCodeInputTooLong, map[string]any{
				"max_length": model.MaxQueryLength,
			})
			return
		}

		qNorm := model.NormalizeQuery(q)
		if qNorm == "" {
			writeError(w, http.StatusBadRequest, ErrCodeMissingQ, nil)
			return
		}
		// Rune-based cap on the normalized form — this is the user-visible
		// limit (200 characters of Korean / ASCII / mixed).
		if utf8.RuneCountInString(qNorm) > model.MaxQueryLength {
			writeError(w, http.StatusRequestURITooLong, ErrCodeInputTooLong, map[string]any{
				"max_length": model.MaxQueryLength,
			})
			return
		}

		typeHint := detectTypeHint(qNorm)
		// "address" hint is reserved for PR-2 — until then, treat as no hint
		// so the existing exact/alias/fuzzy chain still tries.
		dbHint := typeHint
		if dbHint == "address" {
			dbHint = ""
		}

		ctx := r.Context()

		stages := []struct {
			name string
			find func() (*model.Place, error)
		}{
			{"alias_override", func() (*model.Place, error) { return h.Store.FindAliasOverride(ctx, qNorm) }},
			{"exact", func() (*model.Place, error) { return h.Store.FindExact(ctx, qNorm, dbHint) }},
			{"alias", func() (*model.Place, error) { return h.Store.FindByAlias(ctx, qNorm, dbHint) }},
			{"fuzzy", func() (*model.Place, error) { return h.Store.FindByFuzzy(ctx, qNorm, dbHint, FuzzyThreshold) }},
		}

		for _, s := range stages {
			p, err := s.find()
			if err == nil {
				writePlace(w, p)
				return
			}
			if errors.Is(err, model.ErrNotFound) {
				continue
			}
			log.Printf("places resolve %s error: %v", s.name, err)
			writeError(w, http.StatusInternalServerError, ErrCodeInternal, nil)
			return
		}

		writeError(w, http.StatusUnprocessableEntity, ErrCodeUnsupported, map[string]any{
			"input": qNorm,
			"hint":  UnsupportedHint,
		})
	}
}

type resolveResponse struct {
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Source      string  `json:"source"`
	Type        string  `json:"type"`
	NameMatched string  `json:"name_matched"`
}

func writePlace(w http.ResponseWriter, p *model.Place) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resolveResponse{
		Lat:         p.Lat,
		Lon:         p.Lon,
		Source:      p.Source,
		Type:        p.Type,
		NameMatched: p.NameKo,
	})
}

func writeError(w http.ResponseWriter, status int, code string, extra map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"error": code}
	for k, v := range extra {
		body[k] = v
	}
	_ = json.NewEncoder(w).Encode(body)
}
