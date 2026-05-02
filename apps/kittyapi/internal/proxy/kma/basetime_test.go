package kma

import (
	"testing"
	"time"
)

func TestNowToBaseDateTime(t *testing.T) {
	// 2026-04-30 (Thu) is the anchor day for table cases; "yesterday" cases
	// use the prior day. Time zone: Asia/Seoul (KMA upstream is KST-fixed).
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatalf("load Seoul tz: %v", err)
	}

	day := func(h, m int) time.Time {
		return time.Date(2026, 4, 30, h, m, 0, 0, loc)
	}

	tests := []struct {
		name               string
		now                time.Time
		wantDate, wantTime string
	}{
		// Mid-slot — well past the publication delay.
		{"05:30 mid-slot", day(5, 30), "20260430", "0500"},

		// Just before HH:10 — must roll back to the previous slot.
		{"05:09 boundary just before", day(5, 9), "20260430", "0200"},

		// Right at HH:10 — current slot is now usable.
		{"05:10 boundary exact", day(5, 10), "20260430", "0500"},

		// Just after HH:10 — current slot.
		{"05:11 boundary just after", day(5, 11), "20260430", "0500"},

		// Right after midnight — must use yesterday's 23:00 slot.
		{"00:30 just after midnight", day(0, 30), "20260429", "2300"},

		// 02:09 — still yesterday's 23:00 slot.
		{"02:09 last yesterday slot", day(2, 9), "20260429", "2300"},

		// 02:11 — first today slot.
		{"02:11 first today slot", day(2, 11), "20260430", "0200"},

		// Non-slot hour — round down to most recent slot.
		{"13:30 between slots", day(13, 30), "20260430", "1100"},

		// 23:09 — last boundary before final slot.
		{"23:09 before last slot", day(23, 9), "20260430", "2000"},

		// 23:11 — final slot of the day.
		{"23:11 final slot", day(23, 11), "20260430", "2300"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotDate, gotTime := NowToBaseDateTime(tc.now)
			if gotDate != tc.wantDate || gotTime != tc.wantTime {
				t.Errorf("NowToBaseDateTime(%v) = (%s,%s), want (%s,%s)",
					tc.now, gotDate, gotTime, tc.wantDate, tc.wantTime)
			}
		})
	}
}

func TestNowToUltraShortNowcastBaseDateTime(t *testing.T) {
	// 초단기실황 (getUltraSrtNcst): HH:00 발표, ~40분 후 사용 가능.
	// Slot publish hours: every full hour 00..23.
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatalf("load Seoul tz: %v", err)
	}
	day := func(h, m int) time.Time {
		return time.Date(2026, 4, 30, h, m, 0, 0, loc)
	}

	tests := []struct {
		name               string
		now                time.Time
		wantDate, wantTime string
	}{
		// HH:00 — current slot just published, delay not yet elapsed.
		{"05:00 exact (delay not yet)", day(5, 0), "20260430", "0400"},

		// HH:39 — still inside the delay window of the HH:00 slot.
		{"05:39 just before delay window", day(5, 39), "20260430", "0400"},

		// HH:40 — current HH slot becomes usable.
		{"05:40 delay window exact", day(5, 40), "20260430", "0500"},

		// HH:41 — comfortably after delay window.
		{"05:41 just after delay", day(5, 41), "20260430", "0500"},

		// 00:30 — yesterday's 23:00 slot (today's 00:00 not yet usable).
		{"00:30 just after midnight", day(0, 30), "20260429", "2300"},

		// 00:39 — still inside today 00:00 delay → yesterday 23:00.
		{"00:39 still yesterday", day(0, 39), "20260429", "2300"},

		// 00:40 — today's 00:00 slot usable.
		{"00:40 first today slot", day(0, 40), "20260430", "0000"},

		// 02:00 — current slot delay not elapsed → prior hour 01:00.
		{"02:00 current slot delay not yet", day(2, 0), "20260430", "0100"},

		// 02:39 — still inside 02:00 delay → prior hour 01:00.
		{"02:39 prior slot used", day(2, 39), "20260430", "0100"},

		// 02:40 — current 02:00 slot usable.
		{"02:40 current slot usable", day(2, 40), "20260430", "0200"},

		// 19:37 — live verified scenario (KST 2026-04-30 19:37 → 18:00 slot safe).
		{"19:37 live scenario", day(19, 37), "20260430", "1800"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotDate, gotTime := NowToUltraShortNowcastBaseDateTime(tc.now)
			if gotDate != tc.wantDate || gotTime != tc.wantTime {
				t.Errorf("NowToUltraShortNowcastBaseDateTime(%v) = (%s,%s), want (%s,%s)",
					tc.now, gotDate, gotTime, tc.wantDate, tc.wantTime)
			}
		})
	}
}

func TestNowToUltraShortForecastBaseDateTime(t *testing.T) {
	// 초단기예보 (getUltraSrtFcst): HH:30 발표, ~45분 후 사용 가능.
	// Slot publish times: every half hour 00:30, 01:30, ..., 23:30.
	// Delay window: HH:30 + 45min ⇒ HH+1:15.
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatalf("load Seoul tz: %v", err)
	}
	day := func(h, m int) time.Time {
		return time.Date(2026, 4, 30, h, m, 0, 0, loc)
	}

	tests := []struct {
		name               string
		now                time.Time
		wantDate, wantTime string
	}{
		// HH:30 — slot just published, delay not yet elapsed → previous slot.
		{"05:30 just-published not usable", day(5, 30), "20260430", "0430"},

		// HH+1:14 — still inside delay window of HH:30 slot.
		{"06:14 just before delay window", day(6, 14), "20260430", "0430"},

		// HH+1:15 — HH:30 slot becomes usable.
		{"06:15 delay window exact", day(6, 15), "20260430", "0530"},

		// HH+1:16 — comfortably after delay.
		{"06:16 just after delay", day(6, 16), "20260430", "0530"},

		// 00:30 — slot just published, not usable → yesterday 23:30.
		{"00:30 not usable yet", day(0, 30), "20260429", "2330"},

		// 01:14 — still inside 00:30 slot's delay → yesterday 23:30.
		{"01:14 just before usable", day(1, 14), "20260429", "2330"},

		// 01:15 — today's 00:30 slot usable.
		{"01:15 first today slot usable", day(1, 15), "20260430", "0030"},

		// 19:37 — current scenario (live verified): 18:30 slot usable.
		{"19:37 live scenario", day(19, 37), "20260430", "1830"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotDate, gotTime := NowToUltraShortForecastBaseDateTime(tc.now)
			if gotDate != tc.wantDate || gotTime != tc.wantTime {
				t.Errorf("NowToUltraShortForecastBaseDateTime(%v) = (%s,%s), want (%s,%s)",
					tc.now, gotDate, gotTime, tc.wantDate, tc.wantTime)
			}
		})
	}
}
