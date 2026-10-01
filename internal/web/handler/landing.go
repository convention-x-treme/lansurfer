package handler

import (
	"context"

	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
)

// LandingQueries is the public landing page's read model: the leading
// Trending Games with aggregate counts only.
type LandingQueries interface {
	Landing(context.Context) ([]requestquery.LandingGame, error)
}
