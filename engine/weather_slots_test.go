package engine

import "testing"

func TestParseWeatherNowSlots_PlainTextLocationFallback(t *testing.T) {
	slots, err := parseWeatherNowSlots("강남역")
	if err != nil {
		t.Fatalf("parseWeatherNowSlots error: %v", err)
	}
	if slots.LocationQuery != "강남역" {
		t.Fatalf("LocationQuery = %q, want 강남역", slots.LocationQuery)
	}
}

func TestParseWeatherNowSlots_NormalizesKoreanLocationReply(t *testing.T) {
	slots, err := parseWeatherNowSlots("강남역이요.")
	if err != nil {
		t.Fatalf("parseWeatherNowSlots error: %v", err)
	}
	if slots.LocationQuery != "강남역" {
		t.Fatalf("LocationQuery = %q, want 강남역", slots.LocationQuery)
	}
}

func TestParseWeatherNowSlots_RejectsAssistantProseAsLocation(t *testing.T) {
	slots, err := parseWeatherNowSlots("사용자의 질문을 확인해보니 강남역의 현재 날씨를 문의하신 것 같습니다.")
	if err != nil {
		t.Fatalf("parseWeatherNowSlots error: %v", err)
	}
	if slots.LocationQuery != "" {
		t.Fatalf("LocationQuery = %q, want empty", slots.LocationQuery)
	}
}

func TestParseWeatherNowSlots_CompactsSpacedKoreanStationReply(t *testing.T) {
	slots, err := parseWeatherNowSlots("강남 역 이요.")
	if err != nil {
		t.Fatalf("parseWeatherNowSlots error: %v", err)
	}
	if slots.LocationQuery != "강남역" {
		t.Fatalf("LocationQuery = %q, want 강남역", slots.LocationQuery)
	}
}

func TestInferWeatherLocationFromText_KoreanStationWithParticle(t *testing.T) {
	got := inferWeatherLocationFromText("강남역이 비오나? 지금?")
	if got != "강남역" {
		t.Fatalf("inferWeatherLocationFromText = %q, want 강남역", got)
	}
}

func TestLooksLikePlainLocationSlot_RejectsShortEnglishQuestionWords(t *testing.T) {
	for _, text := range []string{"why", "how"} {
		if looksLikePlainLocationSlot(text) {
			t.Fatalf("%q must not be treated as a location follow-up", text)
		}
	}
}
