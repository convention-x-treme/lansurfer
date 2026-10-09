package components

import (
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

func TestRequestTimeUsesActualStartForPlaying(t *testing.T) {
	now := time.Date(2026, 9, 25, 18, 0, 0, 0, time.UTC)
	actual := now.Add(-3 * time.Minute)
	got := RequestTime(gamerequest.StatusPlaying, now.Add(time.Hour), &actual, now)
	if got.Label != "started 3 min ago" || !got.At.Equal(actual) || got.Urgency != UrgencyNone {
		t.Fatalf("RequestTime() = %+v", got)
	}
}
