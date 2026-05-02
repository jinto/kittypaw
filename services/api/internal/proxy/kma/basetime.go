package kma

import (
	"sort"
	"time"
)

// publishHours lists the eight base_time slots KMA publishes the village
// forecast at. Each slot becomes usable about 10 minutes after the wall-clock
// hour (the upstream finalizes the run during that window).
var publishHours = []int{2, 5, 8, 11, 14, 17, 20, 23}

// publishDelay is the minimum age a slot must reach before its data is
// available from KMA.
const publishDelay = 10 * time.Minute

// NowToBaseDateTime maps the current instant to the most recent KMA
// village-forecast publication slot that is already usable. The result is
// the date/time pair to pass to upstream's base_date / base_time params.
//
// Rules:
//   - HH < first slot or HH:00–HH+0:09 (where HH is in publishHours) rolls
//     back to the previous slot.
//   - 00:00–02:09 rolls back to yesterday's 23:00 slot.
//   - Outside slot hours, round down to the most recent published slot.
func NowToBaseDateTime(now time.Time) (baseDate, baseTime string) {
	// Walk back at most one full day; the latest usable slot must lie
	// within (now - publishDelay - 24h, now].
	candidate := now.Add(-publishDelay)
	hour := candidate.Hour()

	// Find the largest publishHours entry ≤ hour. If none, fall back to
	// yesterday's last slot.
	idx := sort.SearchInts(publishHours, hour+1) - 1
	var slotTime time.Time
	if idx >= 0 {
		slotTime = time.Date(candidate.Year(), candidate.Month(), candidate.Day(),
			publishHours[idx], 0, 0, 0, candidate.Location())
	} else {
		yesterday := candidate.AddDate(0, 0, -1)
		slotTime = time.Date(yesterday.Year(), yesterday.Month(), yesterday.Day(),
			publishHours[len(publishHours)-1], 0, 0, 0, candidate.Location())
	}

	baseDate = slotTime.Format("20060102")
	baseTime = slotTime.Format("1504")
	return
}

// ultraShortNowcastDelay is the publication delay for KMA's getUltraSrtNcst —
// slots publish at HH:00 and become usable about 40 minutes later.
const ultraShortNowcastDelay = 40 * time.Minute

// ultraShortForecastDelay is the publication delay for KMA's getUltraSrtFcst —
// slots publish at HH:30 and become usable about 45 minutes later.
const ultraShortForecastDelay = 45 * time.Minute

// nowToHourlyBase computes the most recent KMA slot at HH:slotMinute whose
// publication delay has elapsed. Subtracting the delay yields the latest
// moment by which the slot must already be live; we then round down to the
// nearest HH:slotMinute on or before that moment.
//
// INVARIANT: delay must be < 1 hour. The math relies on subtracting `delay`
// rolling `candidate.Hour()` back at most one hour — a delay ≥ 1h would
// silently skip a publish slot and yield stale data. Today's KMA cadences
// (40 min / 45 min) sit well within this bound.
func nowToHourlyBase(now time.Time, slotMinute int, delay time.Duration) (baseDate, baseTime string) {
	candidate := now.Add(-delay)
	slotTime := time.Date(candidate.Year(), candidate.Month(), candidate.Day(),
		candidate.Hour(), slotMinute, 0, 0, candidate.Location())
	if candidate.Minute() < slotMinute {
		slotTime = slotTime.Add(-time.Hour)
	}
	return slotTime.Format("20060102"), slotTime.Format("1504")
}

// NowToUltraShortNowcastBaseDateTime maps the current instant to the most
// recent KMA ultra-short *nowcast* slot (getUltraSrtNcst) usable now. Slots
// are published every full hour; data becomes available ~40 min after publish.
func NowToUltraShortNowcastBaseDateTime(now time.Time) (baseDate, baseTime string) {
	return nowToHourlyBase(now, 0, ultraShortNowcastDelay)
}

// NowToUltraShortForecastBaseDateTime maps the current instant to the most
// recent KMA ultra-short *forecast* slot (getUltraSrtFcst) usable now. Slots
// are published every half hour at HH:30; data becomes available ~45 min after.
func NowToUltraShortForecastBaseDateTime(now time.Time) (baseDate, baseTime string) {
	return nowToHourlyBase(now, 30, ultraShortForecastDelay)
}
