package components

import (
	"fmt"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

type RequestTimePresentation struct {
	Label   string
	At      time.Time
	Urgency string
}

func RequestTime(status gamerequest.Status, startsAt time.Time, startedAt *time.Time, now time.Time) RequestTimePresentation {
	if status == gamerequest.StatusPlaying {
		if startedAt == nil || startedAt.IsZero() || startedAt.After(now) {
			return RequestTimePresentation{Label: "playing"}
		}
		return historicalRequestTime(*startedAt, now)
	}
	if status == gamerequest.StatusClosed {
		if startedAt != nil && !startedAt.IsZero() && !startedAt.After(now) {
			return historicalRequestTime(*startedAt, now)
		}
		if startsAt.IsZero() {
			return RequestTimePresentation{}
		}
		return RequestTimePresentation{Label: "planned " + startsAt.UTC().Format("Jan 2"), At: startsAt}
	}
	if startsAt.IsZero() {
		return RequestTimePresentation{}
	}
	if status == gamerequest.StatusCancelled {
		return RequestTimePresentation{Label: "planned " + startsAt.UTC().Format("Jan 2"), At: startsAt}
	}
	return RequestTimePresentation{Label: "starts " + plannedStartLabel(startsAt, now), At: startsAt, Urgency: plannedStartUrgency(startsAt, now)}
}

func historicalRequestTime(at, now time.Time) RequestTimePresentation {
	elapsed := now.Sub(at)
	label := "started just now"
	switch {
	case elapsed < 0:
		return RequestTimePresentation{Label: "playing"}
	case elapsed < time.Minute:
	case elapsed < time.Hour:
		label = fmt.Sprintf("started %d min ago", max(1, int(elapsed/time.Minute)))
	case elapsed < 24*time.Hour:
		label = fmt.Sprintf("started %d h ago", max(1, int(elapsed/time.Hour)))
	default:
		label = "started " + at.UTC().Format("Jan 2")
	}
	return RequestTimePresentation{Label: label, At: at}
}

func plannedStartUrgency(startsAt, now time.Time) string {
	remaining := startsAt.Sub(now)
	switch {
	case remaining <= 5*time.Minute:
		return UrgencyStarting
	case remaining <= 15*time.Minute:
		return UrgencySoon
	default:
		return UrgencyNone
	}
}

func plannedStartLabel(startsAt, now time.Time) string {
	remaining := startsAt.Sub(now)
	if remaining <= 2*time.Minute {
		return "now"
	}
	if remaining < time.Hour {
		return fmt.Sprintf("in %d min", max(1, int(remaining.Round(time.Minute)/time.Minute)))
	}
	if remaining < 24*time.Hour {
		return fmt.Sprintf("in %d h", max(1, int(remaining.Round(time.Hour)/time.Hour)))
	}
	return startsAt.UTC().Format("Jan 2") + " at " + startsAt.UTC().Format("15:04")
}
