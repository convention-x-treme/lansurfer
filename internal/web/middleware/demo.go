package middleware

import (
	"context"
	"net/http"
	"time"
)

type demoKey struct{}

// DemoState is what the shared layouts need to render the demo banner.
type DemoState struct {
	NextReset time.Time
	// Notice is a demo-specific post-action key (demo-forbidden or
	// demo-admin-granted), shown in the banner so every redirect target
	// renders it without its own notice mapping.
	Notice string
}

// DemoSchedule is the configured daily demo reset wall-clock time.
type DemoSchedule struct {
	Hour, Minute int
	Location     *time.Location
}

// Demo carries the next daily reset instant into every request's context the
// same way Instance carries the instance name. NewRouter applies it only in
// demo mode; without it DemoBanner reports false and no banner renders.
func Demo(schedule DemoSchedule) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			state := DemoState{NextReset: NextDemoReset(time.Now(), schedule.Hour, schedule.Minute, schedule.Location)}
			switch notice := r.URL.Query().Get("notice"); notice {
			case "demo-forbidden", "demo-admin-granted":
				state.Notice = notice
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), demoKey{}, state)))
		})
	}
}

// DemoBanner returns the state carried by Demo; false outside demo mode.
func DemoBanner(ctx context.Context) (DemoState, bool) {
	state, ok := ctx.Value(demoKey{}).(DemoState)
	return state, ok
}

// NextDemoReset is the first hour:minute wall-clock instant in location
// strictly after now. time.Date normalizes a wall time skipped by a DST jump
// forward and picks one of the two instants of a repeated hour.
func NextDemoReset(now time.Time, hour, minute int, location *time.Location) time.Time {
	local := now.In(location)
	reset := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, location)
	if !reset.After(now) {
		reset = time.Date(local.Year(), local.Month(), local.Day()+1, hour, minute, 0, 0, location)
	}
	return reset
}
