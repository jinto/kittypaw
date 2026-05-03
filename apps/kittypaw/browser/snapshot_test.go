package browser

import (
	"strings"
	"testing"
)

func TestTruncateRunes(t *testing.T) {
	got := truncateRunes(strings.Repeat("가", 13000), 12000)
	if len([]rune(got)) != 12014 {
		t.Fatalf("rune len = %d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "...(truncated)") {
		t.Fatalf("missing suffix: %q", got[len(got)-20:])
	}
}

func TestElementRefsAreStable(t *testing.T) {
	elements := []snapshotElement{{Role: "link", Text: "Docs", Selector: "a:nth-of-type(1)"}}
	assignRefs(elements)
	if elements[0].Ref != "e1" {
		t.Fatalf("ref = %q", elements[0].Ref)
	}
}
