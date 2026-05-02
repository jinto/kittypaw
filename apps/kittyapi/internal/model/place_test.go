package model

import "testing"

func TestNormalizeQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		{"plain", "강남역", "강남역"},
		{"trim outer", "  강남역  ", "강남역"},
		{"collapse double space", "강남  역", "강남 역"},
		{"tab to space", "강남\t역", "강남 역"},
		{"newline to space", "강남\n역", "강남 역"},
		{"mixed runs", "강남 \t  역", "강남 역"},
		{"address", "  서울 강남구 테헤란로 152  ", "서울 강남구 테헤란로 152"},
		// NFD form (decomposed Hangul) → NFC: '가' (NFC: 0xAC00) vs '가' (NFD)
		{"NFD to NFC", "가", "가"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NormalizeQuery(c.in); got != c.want {
				t.Errorf("NormalizeQuery(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestPlaceTypeConstants(t *testing.T) {
	// Ensure no typos in shared constants.
	if TypeSubwayStation != "subway_station" {
		t.Errorf("TypeSubwayStation = %q", TypeSubwayStation)
	}
	if TypeLandmark != "landmark" {
		t.Errorf("TypeLandmark = %q", TypeLandmark)
	}
	if SourceWikidata != "wikidata" {
		t.Errorf("SourceWikidata = %q", SourceWikidata)
	}
	if MaxQueryLength != 200 {
		t.Errorf("MaxQueryLength = %d", MaxQueryLength)
	}
}
