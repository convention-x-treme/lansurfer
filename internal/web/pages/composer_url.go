package pages

import (
	"net/url"

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

func gameRequestSelectionQuery(item game.DiscoveryItem) string {
	return gameRequestSelectionValues(item).Encode()
}

func gameRequestComposerURL(item game.DiscoveryItem, form GameRequestForm) string {
	values := gameRequestSelectionValues(item)
	addComposerDetails(values, form)
	return "/game-requests/new?" + values.Encode()
}

func gameRequestSelectionURL(item game.DiscoveryItem) string {
	return "/game-requests/new/selection?" + gameRequestSelectionQuery(item)
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
