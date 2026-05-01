package server

import (
	"errors"
	"net/http"
	"testing"
)

func TestNormalizeListenAndServeErrorTreatsServerClosedAsNil(t *testing.T) {
	if err := normalizeListenAndServeError(http.ErrServerClosed); err != nil {
		t.Fatalf("normalizeListenAndServeError(http.ErrServerClosed) = %v, want nil", err)
	}
}

func TestNormalizeListenAndServeErrorPreservesOtherErrors(t *testing.T) {
	want := errors.New("bind failed")
	if got := normalizeListenAndServeError(want); got != want {
		t.Fatalf("normalizeListenAndServeError() = %v, want %v", got, want)
	}
}
