package engine

import "strings"

const openMeteoAttributionLine = "Weather data by Open-Meteo.com (https://open-meteo.com)"

// normalizePackageOutputAttribution keeps legacy installed official packages
// aligned with the current output policy: no KittyPaw brand footer, and no
// provider footer unless attribution is required.
func normalizePackageOutputAttribution(packageID, output string) string {
	if output == "" {
		return output
	}
	lines := strings.Split(output, "\n")
	out := make([]string, 0, len(lines))
	needsOpenMeteoAttribution := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, line)
			continue
		}
		if strings.Contains(trimmed, "Open-Meteo") && strings.Contains(trimmed, "Powered by KittyPaw") {
			needsOpenMeteoAttribution = true
			continue
		}
		if isLegacyOptionalAttributionLine(packageID, trimmed) {
			continue
		}
		if strings.Contains(trimmed, "Powered by KittyPaw") {
			cleaned := strings.ReplaceAll(line, " · Powered by KittyPaw", "")
			cleaned = strings.ReplaceAll(cleaned, "Powered by KittyPaw", "")
			cleaned = strings.TrimSpace(cleaned)
			cleaned = strings.Trim(cleaned, "_")
			if cleaned == "" {
				continue
			}
			out = append(out, cleaned)
			continue
		}
		out = append(out, line)
	}
	normalized := strings.TrimSpace(strings.Join(out, "\n"))
	if needsOpenMeteoAttribution && !strings.Contains(normalized, openMeteoAttributionLine) {
		if normalized != "" {
			normalized += "\n\n"
		}
		normalized += openMeteoAttributionLine
	}
	return normalized
}

func isLegacyOptionalAttributionLine(packageID, line string) bool {
	if !strings.HasPrefix(line, "_") || !strings.Contains(line, ":") {
		return false
	}
	switch packageID {
	case "weather-now", "weather-soon":
		return containsAny(line, "wttr.in", "기상청", "KMA")
	case "exchange-rate":
		return containsAny(line, "Frankfurter", "ECB")
	case "weather-briefing":
		return containsAny(line, "기상청", "KMA")
	}
	return false
}
