package engine

import (
	"strings"
	"testing"
)

func TestNormalizePackageOutputAttribution_StripsLegacyOptionalFooters(t *testing.T) {
	out := normalizePackageOutputAttribution("weather-now", strings.Join([]string{
		"🌤 Seoul 날씨",
		"",
		"Sunny",
		"",
		"_Source: wttr.in · Powered by KittyPaw_",
	}, "\n"))

	if strings.Contains(out, "wttr.in") || strings.Contains(out, "Powered by KittyPaw") || strings.Contains(out, "_Source:") {
		t.Fatalf("legacy optional attribution should be removed:\n%s", out)
	}
	if !strings.Contains(out, "Sunny") {
		t.Fatalf("weather facts should be preserved:\n%s", out)
	}
}

func TestNormalizePackageOutputAttribution_RewritesOpenMeteoRequiredFooter(t *testing.T) {
	out := normalizePackageOutputAttribution("weather-briefing", strings.Join([]string{
		"🌤 *Weather Briefing — Seoul*",
		"",
		"_Data: Open-Meteo · Powered by KittyPaw_",
	}, "\n"))

	if !strings.Contains(out, "Weather data by Open-Meteo.com") {
		t.Fatalf("required Open-Meteo attribution missing:\n%s", out)
	}
	if strings.Contains(out, "Powered by KittyPaw") || strings.Contains(out, "_Data:") {
		t.Fatalf("legacy branding should be removed:\n%s", out)
	}
}
