package handler

import (
	"context"
	"net/http"

	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/web/components"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
)

// upcomingJoinedLimit bounds the navigation's "Your next games" list. Past
// that, the Game Requests screen itself is the place to look, so the sidebar
// stays a shortcut rather than a second list.
const upcomingJoinedLimit = 3

// NavigationQueries is the sidebar's own read-only view of the Game Request
// read model: the installation-wide open count beside "Game Requests", the
// viewer's joined requests for "Your next games", and their reading history
// for "Recently viewed".
type NavigationQueries interface {
	CountOpen(context.Context) (int64, error)
	ListActive(context.Context, string, requestquery.Filter) ([]requestquery.Request, error)
	RecentlyViewed(context.Context, string) ([]requestquery.RecentRequest, error)
}

// NavigationCatalog is the sidebar's own read-only view of the Game catalog:
// the two counts it states beside Games and My Games, never the lists.
type NavigationCatalog interface {
	CountCatalog(context.Context) (int64, error)
	CountFollowed(context.Context, string) (int64, error)
}

// Navigation builds the shared sidebar state every authenticated screen
// renders. It exists so the counts and lists mean the same thing on every
// screen: before it, only the Game Request list populated the open-request
// badge, so the same sidebar silently lost its numbers as soon as the viewer
// navigated anywhere else.
type Navigation struct {
	queries                NavigationQueries
	catalog                NavigationCatalog
	discordDeliveryEnabled bool
}

func NewNavigation(queries NavigationQueries, catalog NavigationCatalog, discordDeliveryEnabled bool) *Navigation {
	return &Navigation{queries: queries, catalog: catalog, discordDeliveryEnabled: discordDeliveryEnabled}
}

// For assembles the sidebar for one request and marks section as current.
//
// Every read here is decoration around a screen that has already succeeded,
// so a failing one degrades to an absent count or an empty list instead of
// replacing the page the viewer asked for with an error. The failure is
// logged, not swallowed silently.
func (n *Navigation) For(r *http.Request, section string) components.NavigationState {
	account, ok := webmiddleware.CurrentUser(r.Context())
	if !ok {
		return components.NavigationState{Section: section}
	}
	state := components.NavigationState{
		Viewer:                 &components.Viewer{ID: account.ID, GameNick: account.GameNick, Role: string(account.Role)},
		Section:                section,
		DiscordDeliveryEnabled: n != nil && n.discordDeliveryEnabled,
	}
	if n == nil {
		return state
	}
	ctx := r.Context()
	// Each source is checked on its own so a caller that wires only some of
	// them -- a focused test, or a future screen with a narrower shell --
	// renders the sidebar it can rather than panicking on the one it cannot.
	if n.queries != nil {
		if open, err := n.queries.CountOpen(ctx); err != nil {
			webmiddleware.LogError(r, "count open Game Requests for navigation", err)
		} else {
			state.OpenRequests = open
		}
		state.Upcoming = n.upcoming(r, account.ID)
		if recent, err := n.queries.RecentlyViewed(ctx, account.ID); err != nil {
			webmiddleware.LogError(r, "list recently viewed Game Requests for navigation", err)
		} else {
			state.RecentlyViewed = recent
		}
	}
	if n.catalog != nil {
		if total, err := n.catalog.CountCatalog(ctx); err != nil {
			webmiddleware.LogError(r, "count catalog Games for navigation", err)
		} else {
			state.Games = total
		}
		if followed, err := n.catalog.CountFollowed(ctx, account.ID); err != nil {
			webmiddleware.LogError(r, "count followed Games for navigation", err)
		} else {
			state.FollowedGames = followed
		}
	}
	return state
}

// upcoming lists the open Game Requests the viewer has joined, soonest first.
// The read model already orders by start time, so this only bounds the list.
func (n *Navigation) upcoming(r *http.Request, viewerID string) []requestquery.Request {
	joined, err := n.queries.ListActive(r.Context(), viewerID, requestquery.Filter{Scope: requestquery.ScopeJoined})
	if err != nil {
		webmiddleware.LogError(r, "list joined Game Requests for navigation", err)
		return nil
	}
	if len(joined) > upcomingJoinedLimit {
		joined = joined[:upcomingJoinedLimit]
	}
	return joined
}
