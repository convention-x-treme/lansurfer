package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

func TestParseCreateFormUsesBrowserTimezone(t *testing.T) {
	handler := &GameRequests{now: func() time.Time { return time.Date(2027, 5, 1, 8, 0, 0, 0, time.UTC) }}
	form := url.Values{
		"game_id":         {"game-1"},
		"max_players":     {"4"},
		"starts_at":       {"2027-05-01T18:30"},
		"timezone_offset": {"-120"},
		"description":     {" Casual players "},
	}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	parsedForm, maximum, startsAt, err := handler.parseCreateForm(request)
	if err != nil {
		t.Fatalf("parseCreateForm() error = %v", err)
	}
	if parsedForm.TimezoneOffset != "-120" || maximum == nil || *maximum != 4 {
		t.Fatalf("form/maximum = %#v, %#v", parsedForm, maximum)
	}
	want := time.Date(2027, 5, 1, 16, 30, 0, 0, time.UTC)
	if !startsAt.Equal(want) {
		t.Errorf("startsAt = %s, want %s", startsAt, want)
	}
}

func TestParseCreateFormSupportsStartPresets(t *testing.T) {
	now := time.Date(2027, 5, 1, 8, 0, 0, 0, time.UTC)
	handler := &GameRequests{now: func() time.Time { return now }}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(url.Values{
		"game_id": {"game-1"}, "start_preset": {"30"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, _, startsAt, err := handler.parseCreateForm(request)
	if err != nil {
		t.Fatalf("parseCreateForm() error = %v", err)
	}
	if want := now.Add(30 * time.Minute); !startsAt.Equal(want) {
		t.Errorf("startsAt = %s, want %s", startsAt, want)
	}
}

func TestParseCreateFormRequiresCompleteCustomStart(t *testing.T) {
	handler := &GameRequests{now: time.Now}
	request := httptest.NewRequest(http.MethodPost, "/game-requests", strings.NewReader(url.Values{
		"game_id": {"game-1"}, "start_preset": {"custom"}, "start_date": {"2027-05-01"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	if _, _, _, err := handler.parseCreateForm(request); !errors.Is(err, gamerequest.ErrInvalidStartsAt) {
		t.Fatalf("parseCreateForm() error = %v", err)
	}
}

func TestGameRequestErrorStatus(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{gamerequest.ErrNotFound, http.StatusNotFound},
		{gamerequest.ErrNotCreator, http.StatusForbidden},
		{gamerequest.ErrFull, http.StatusConflict},
		{gamerequest.ErrInvalidDescription, http.StatusUnprocessableEntity},
		{errors.New("database unavailable"), http.StatusInternalServerError},
	}
	for _, test := range tests {
		if got := gameRequestErrorStatus(test.err); got != test.want {
			t.Errorf("gameRequestErrorStatus(%v) = %d, want %d", test.err, got, test.want)
		}
	}
}
