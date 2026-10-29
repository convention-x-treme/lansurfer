package middleware

import (
	"testing"
	"time"
)

func TestNextDemoResetAcrossDST(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	utc := func(value string) time.Time {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	for _, tc := range []struct {
		name         string
		now          string
		hour, minute int
		want         string
	}{
		{"same day before reset", "2026-10-24T01:00:00Z", 4, 0, "2026-10-24T02:00:00Z"},
		{"into autumn DST end", "2026-10-24T12:00:00Z", 4, 0, "2026-10-25T03:00:00Z"},
		{"exactly at reset rolls over", "2026-10-25T03:00:00Z", 4, 0, "2026-10-26T03:00:00Z"},
		{"into spring DST start", "2027-03-27T12:00:00Z", 4, 0, "2027-03-28T02:00:00Z"},
		{"skipped wall time", "2027-03-27T12:00:00Z", 2, 30, "2027-03-28T01:30:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := NextDemoReset(utc(tc.now), tc.hour, tc.minute, berlin)
			if !got.Equal(utc(tc.want)) {
				t.Errorf("NextDemoReset(%s) = %s, want %s", tc.now, got.UTC().Format(time.RFC3339), tc.want)
			}
		})
	}
}
