package server

import (
	"strings"
	"testing"
)

func TestManualAppDoesNotExposeRawHTMLErrors(t *testing.T) {
	raw, err := manualAssets.ReadFile("manual/app.js")
	if err != nil {
		t.Fatalf("read manual app: %v", err)
	}
	src := string(raw)
	if strings.Contains(src, "body = { error: text };") {
		t.Fatal("manual UI must not render raw non-JSON response bodies as chat messages")
	}
	if !strings.Contains(src, "formatHTTPError") {
		t.Fatal("manual UI must format non-JSON HTTP errors before showing them")
	}
}

func TestManualAppReplacesStaleSelectedRoute(t *testing.T) {
	raw, err := manualAssets.ReadFile("manual/app.js")
	if err != nil {
		t.Fatalf("read manual app: %v", err)
	}
	src := string(raw)
	if strings.Contains(src, "state.deviceID = state.deviceID || first.device_id || \"\";") {
		t.Fatal("manual UI must not keep a selected device that is absent from freshly loaded routes")
	}
	if !strings.Contains(src, "selectFirstAvailableRoute") {
		t.Fatal("manual UI must choose an available route after loading routes")
	}
}
