package pages

import (
	"net/url"
	"strings"

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

// gameRequestSelectionQuery is the same encoding as a query string, for
// Games' "Find Players" link into the composer.
func gameRequestSelectionQuery(item game.DiscoveryItem) string {
	return gameRequestSelectionValues(item).Encode()
}

func gameRequestComposerURL(item game.DiscoveryItem, form GameRequestForm) string {
	values := gameRequestSelectionValues(item)
	addComposerDetails(values, form)
	return "/game-requests/new?" + values.Encode()
}

// composerChangeGameURL is "Change game": the composer's own pick list
// again, carrying every request detail already typed and no selection, so
// the player picks a replacement and comes back to a form that still knows
// their max players, start time, and description.
func composerChangeGameURL(form GameRequestForm) string {
	values := url.Values{}
	addComposerDetails(values, form)
	if len(values) == 0 {
		return "/game-requests/new"
	}
	return "/game-requests/new?" + values.Encode()
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

// describedBy lists the ids that describe one control -- its standing hint,
// plus its error when one is showing -- and renders nothing at all when
// there is neither, rather than an empty aria-describedby.
func describedBy(hintID string, hasError bool, errorID string) templ.Attributes {
	ids := make([]string, 0, 2)
	if hasError {
		ids = append(ids, errorID)
	}
	if hintID != "" {
		ids = append(ids, hintID)
	}
	if len(ids) == 0 {
		return templ.Attributes{}
	}
	return templ.Attributes{"aria-describedby": strings.Join(ids, " ")}
}

// fieldState is describedBy plus the state a rejected control must carry:
// aria-invalid spelled "true" (an empty aria-invalid reads as valid to a
// screen reader), and autofocus so the re-rendered page opens on the field
// that needs the correction -- no script required.
func fieldState(hintID string, hasError bool, errorID string) templ.Attributes {
	attrs := describedBy(hintID, hasError, errorID)
	if hasError {
		attrs["aria-invalid"] = "true"
		attrs["autofocus"] = true
	}
	return attrs
}

// coverLoading marks the one cover a screen opens on as eager; every other
// cover stays lazy.
func coverLoading(priority bool) templ.Attributes {
	if priority {
		return templ.Attributes{"loading": "eager", "fetchpriority": "high"}
	}
	return templ.Attributes{"loading": "lazy"}
}
