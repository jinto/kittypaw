package proxy

// Resolve endpoint error codes (single source of truth — Critic ITERATE #2).
// Stable identifiers exposed in the JSON response `error` field; clients
// branch on these strings rather than HTTP status when needed.
const (
	ErrCodeMissingQ     = "missing_q"
	ErrCodeInputTooLong = "input_too_long"
	ErrCodeUnsupported  = "unsupported_input"
	ErrCodeInternal     = "internal_error"
)

// UnsupportedHint is returned in 422 responses to guide LLM clients to
// retry with a normalized form (nearest subway station / road address).
const UnsupportedHint = "지원: ○○역, 도로명주소, 지번주소, 한국 주요 랜드마크. 상호명·체인점은 미지원 — 가까운 지하철역으로 다시 시도해주세요."
