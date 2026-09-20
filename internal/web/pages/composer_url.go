package pages

import (
	"net/url"

	"github.com/a-h/templ"
	"github.com/convention-x-treme/lansurfer/internal/game"
)

// gameRequestSelectionValues is the single template-side encoding boundary
// for an opaque composer selection. Names and provider presentation never
// cross this browser boundary.
func gameRequestSelectionValues(item game.DiscoveryItem) url.Values {
	values := url.Values{}
	if item.LocalGameID != "" {
		values.Set("game_id", item.LocalGameID)
	} else {
		values.Set("provider", item.Reference.Provider)
		values.Set("external_id", item.Reference.ExternalID)
	}
	return values
}

// GameRequestSelectionQuery is the exported form of the same encoding
// boundary, reused by the handler package for the composer's non-htmx
// selection redirect so the identity contract is defined in exactly one
// place.
func GameRequestSelectionQuery(item game.DiscoveryItem) string {
	return gameRequestSelectionValues(item).Encode()
}

func gameRequestComposerURL(item game.DiscoveryItem, form GameRequestForm) string {
	values := gameRequestSelectionValues(item)
	addComposerDetails(values, form)
	return "/game-requests/new?" + values.Encode()
}

func gameRequestSelectionURL(item game.DiscoveryItem) string {
	return "/game-requests/new/selection?" + GameRequestSelectionQuery(item)
}

func addComposerDetails(values url.Values, form GameRequestForm) {
	for key, value := range map[string]string{
		"max_players": form.MaxPlayers, "start_preset": form.StartPreset,
		"start_date": form.StartDate, "start_time": form.StartTime,
		"starts_at": form.StartsAt, "timezone_offset": form.TimezoneOffset,
		"description": form.Description,
	} {
		if value != "" {
			values.Set(key, value)
		}
	}
}

// gameRequestPickEmpty supplies GameDiscoveryState's empty panel only for
// the composer's pre-search list, where "nothing to show" means "you follow
// nothing yet". A search that matched nothing keeps GameDiscoveryState's own
// "No games found for ..." message instead.
func gameRequestPickEmpty(search string) templ.Component {
	if search != "" {
		return nil
	}
	return GameRequestPickEmpty()
}

// composerSearchInputID keeps the first-pick search and the "Change game"
// search distinct ids: each form's live-search trigger names its own input,
// and the two never coexist in one document.
func composerSearchInputID(changeGame bool) string {
	if changeGame {
		return "game-request-change-search"
	}
	return "game-request-search"
}

// composerSearchTrigger is Games' live-search trigger (Task 4), including
// the type=search clear button's own "search" event, so clearing the field
// returns to the followed list without a submit.
func composerSearchTrigger(changeGame bool) string {
	id := "#" + composerSearchInputID(changeGame)
	return "submit, input changed delay:300ms from:(" + id + "), search from:(" + id + ")"
}
