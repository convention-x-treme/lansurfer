package components

import (
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

func TestRequestTimeLifecycle(t *testing.T) {
	now := time.Date(2026, 9, 25, 18, 0, 0, 0, time.UTC)
	plan := now.Add(time.Hour)
	actual := now.Add(-3 * time.Minute)
	zero := time.Time{}
	future := now.Add(time.Minute)
	tests := []struct {
		name    string
		status  gamerequest.Status
		plan    time.Time
		actual  *time.Time
		label   string
		at      time.Time
		urgency string
	}{
		{"open", gamerequest.StatusOpen, plan, &actual, "starts in 1 h", plan, UrgencyNone},
		{"open beyond a day", gamerequest.StatusOpen, now.Add(48*time.Hour + 90*time.Minute), nil, "starts Sep 27 at 19:30", now.Add(48*time.Hour + 90*time.Minute), UrgencyNone},
		{"open at 15 minutes", gamerequest.StatusOpen, now.Add(15 * time.Minute), nil, "starts in 15 min", now.Add(15 * time.Minute), UrgencySoon},
		{"open at 5 minutes", gamerequest.StatusOpen, now.Add(5 * time.Minute), nil, "starts in 5 min", now.Add(5 * time.Minute), UrgencyStarting},
		{"open at 2 minutes", gamerequest.StatusOpen, now.Add(2 * time.Minute), nil, "starts now", now.Add(2 * time.Minute), UrgencyStarting},
		{"open overdue", gamerequest.StatusOpen, now.Add(-time.Minute), nil, "starts now", now.Add(-time.Minute), UrgencyStarting},
		{"open overdue by 10 minutes", gamerequest.StatusOpen, now.Add(-10 * time.Minute), nil, "planned start: 10 min ago", now.Add(-10 * time.Minute), UrgencyNone},
		{"open overdue by 1 hour", gamerequest.StatusOpen, now.Add(-time.Hour), &actual, "planned start: 1 h ago", now.Add(-time.Hour), UrgencyNone},
		{"open overdue by a day", gamerequest.StatusOpen, now.Add(-24 * time.Hour), nil, "planned start: Sep 24", now.Add(-24 * time.Hour), UrgencyNone},
		{"playing", gamerequest.StatusPlaying, plan, &actual, "started 3 min ago", actual, UrgencyNone},
		{"playing missing actual", gamerequest.StatusPlaying, plan, nil, "playing", time.Time{}, UrgencyNone},
		{"playing zero actual", gamerequest.StatusPlaying, plan, &zero, "playing", time.Time{}, UrgencyNone},
		{"playing future actual", gamerequest.StatusPlaying, plan, &future, "playing", time.Time{}, UrgencyNone},
		{"closed actual", gamerequest.StatusClosed, plan, &actual, "started 3 min ago", actual, UrgencyNone},
		{"closed missing actual", gamerequest.StatusClosed, plan, nil, "planned Sep 25", plan, UrgencyNone},
		{"closed zero actual", gamerequest.StatusClosed, plan, &zero, "planned Sep 25", plan, UrgencyNone},
		{"closed future actual", gamerequest.StatusClosed, plan, &future, "planned Sep 25", plan, UrgencyNone},
		{"closed zero plan", gamerequest.StatusClosed, time.Time{}, nil, "", time.Time{}, UrgencyNone},
		{"closed actual without plan", gamerequest.StatusClosed, time.Time{}, &actual, "started 3 min ago", actual, UrgencyNone},
		{"closed without valid times", gamerequest.StatusClosed, time.Time{}, &zero, "", time.Time{}, UrgencyNone},
		{"cancelled", gamerequest.StatusCancelled, plan, &actual, "planned Sep 25", plan, UrgencyNone},
		{"cancelled zero plan", gamerequest.StatusCancelled, time.Time{}, nil, "", time.Time{}, UrgencyNone},
		{"open zero plan", gamerequest.StatusOpen, time.Time{}, nil, "", time.Time{}, UrgencyNone},
		{"closed actual just started", gamerequest.StatusClosed, plan, new(now), "started just now", now, UrgencyNone},
		{"closed actual one minute", gamerequest.StatusClosed, plan, new(now.Add(-time.Minute)), "started 1 min ago", now.Add(-time.Minute), UrgencyNone},
		{"closed actual one hour", gamerequest.StatusClosed, plan, new(now.Add(-time.Hour)), "started 1 h ago", now.Add(-time.Hour), UrgencyNone},
		{"closed actual one day", gamerequest.StatusClosed, plan, new(now.Add(-24 * time.Hour)), "started Sep 24", now.Add(-24 * time.Hour), UrgencyNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RequestTime(tt.status, tt.plan, tt.actual, now)
			if got.Label != tt.label || !got.At.Equal(tt.at) || got.Urgency != tt.urgency {
				t.Fatalf("RequestTime() = %+v; want label=%q at=%v urgency=%q", got, tt.label, tt.at, tt.urgency)
			}
		})
	}
}
