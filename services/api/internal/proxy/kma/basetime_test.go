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
