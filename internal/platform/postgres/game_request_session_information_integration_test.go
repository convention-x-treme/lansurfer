//go:build integration

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

func TestGameRequestSessionInformationReadReturnsDatabaseErrors(t *testing.T) {
	f := newPlayingFixture(t)
	req := f.create(t, "cancelled read")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := f.requests.SessionInformation(ctx, req.ID, f.host)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled read error = %v, want context.Canceled", err)
	}
}

func TestGameRequestSessionInformationReadAccess(t *testing.T) {
	f := newPlayingFixture(t)
	req := f.create(t, "session info")
	input := gamerequest.SessionInformationInput{Key: "Server", Value: "10.20.30.40:27015"}
	if err := f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.SessionInformation(t.Context(), req.ID, f.outsider); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("outsider read = %v", err)
	}
	if err := f.requests.Join(t.Context(), req.ID, f.guest); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{f.host, f.guest} {
		got, err := f.requests.SessionInformation(t.Context(), req.ID, user)
		if err != nil || len(got.Entries) != 1 || got.Entries[0].Value != input.Value {
			t.Fatalf("read = %+v, %v", got, err)
		}
	}
	if err := f.requests.Leave(t.Context(), req.ID, f.guest); err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.SessionInformation(t.Context(), req.ID, f.guest); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("after leave = %v", err)
	}
	if err := f.requests.Join(t.Context(), req.ID, f.guest); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.Start(t.Context(), req.ID, f.host); err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.SessionInformation(t.Context(), req.ID, f.guest); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.Close(t.Context(), req.ID, f.host); err != nil {
		t.Fatal(err)
	}
	if _, err := f.requests.SessionInformation(t.Context(), req.ID, f.host); !errors.Is(err, gamerequest.ErrNotActive) {
		t.Fatalf("closed read = %v", err)
	}
}

func TestGameRequestSessionInformationCreateValidationAndDatabaseConstraints(t *testing.T) {
	f := newPlayingFixture(t)
	first := f.create(t, "first")
	second := f.create(t, "second")
	if err := f.requests.CreateSessionInformationEntry(t.Context(), first.ID, f.host, gamerequest.SessionInformationInput{Key: " Server ", Value: "  host "}); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.CreateSessionInformationEntry(t.Context(), first.ID, f.host, gamerequest.SessionInformationInput{Key: "server", Value: "duplicate"}); !errors.Is(err, gamerequest.ErrSessionInformationKeyExists) {
		t.Fatalf("duplicate = %v", err)
	}
	if err := f.requests.CreateSessionInformationEntry(t.Context(), second.ID, f.host, gamerequest.SessionInformationInput{Key: "server", Value: "another"}); err != nil {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{strings.Repeat("k", 41), "v"}, {"k", strings.Repeat("v", 301)}, {"k\nx", "v"}, {"k", "v\r"}, {"", "v"}, {"k", ""}} {
		if _, err := f.pool.Exec(t.Context(), `INSERT INTO game_request_session_entries(game_request_id,key,value) VALUES ($1,$2,$3)`, first.ID, pair[0], pair[1]); err == nil {
			t.Errorf("database accepted key/value lengths %d/%d", len(pair[0]), len(pair[1]))
		}
	}
	var key, value string
	if err := f.pool.QueryRow(t.Context(), `SELECT key,value FROM game_request_session_entries WHERE game_request_id=$1`, first.ID).Scan(&key, &value); err != nil || key != "Server" || value != "host" {
		t.Fatalf("stored = %q/%q, %v", key, value, err)
	}
}

func TestGameRequestSessionInformationUpdateDeleteAndOrdering(t *testing.T) {
	f := newPlayingFixture(t)
	req := f.create(t, "mutation")
	ctx := t.Context()
	for _, input := range []gamerequest.SessionInformationInput{{Key: "Server", Value: "one"}, {Key: "Lobby", Value: "two"}, {Key: "Meeting", Value: "three"}} {
		if err := f.requests.CreateSessionInformationEntry(ctx, req.ID, f.host, input); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := f.requests.SessionInformation(ctx, req.ID, f.host)
	if err != nil || len(rows.Entries) != 3 {
		t.Fatalf("entries = %+v, %v", rows, err)
	}
	original := rows.Entries[1]
	if err := f.requests.UpdateSessionInformationEntry(ctx, req.ID, original.ID, f.host, gamerequest.SessionInformationInput{Key: "Room", Value: "changed"}); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.UpdateSessionInformationEntry(ctx, req.ID, original.ID, f.host, gamerequest.SessionInformationInput{Key: " Room ", Value: " changed "}); err != nil {
		t.Fatal(err)
	}
	rows, err = f.requests.SessionInformation(ctx, req.ID, f.host)
	if err != nil || rows.Entries[1].ID != original.ID || rows.Entries[1].Sequence != original.Sequence || rows.Entries[1].Key != "Room" {
		t.Fatalf("updated entries = %+v, %v", rows, err)
	}
	deleted := rows.Entries[0].ID
	if err := f.requests.DeleteSessionInformationEntry(ctx, req.ID, deleted, f.host); err != nil {
		t.Fatal(err)
	}
	if err := f.requests.DeleteSessionInformationEntry(ctx, req.ID, deleted, f.host); !errors.Is(err, gamerequest.ErrSessionInformationEntryNotFound) {
		t.Fatalf("repeated delete = %v", err)
	}
}

func TestGameRequestSessionInformationLimitSerializesConcurrentCreates(t *testing.T) {
	f := newPlayingFixture(t)
	req := f.create(t, "limit")
	for i := range 11 {
		if err := f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, gamerequest.SessionInformationInput{Key: fmt.Sprintf("key-%d", i), Value: "v"}); err != nil {
			t.Fatal(err)
		}
	}
	results := runConcurrently(func() error {
		return f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, gamerequest.SessionInformationInput{Key: "race-1", Value: "v"})
	}, func() error {
		return f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, gamerequest.SessionInformationInput{Key: "race-2", Value: "v"})
	})
	var successes, limits int
	for _, err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, gamerequest.ErrSessionInformationLimit) {
			limits++
		} else {
			t.Fatalf("create error = %v", err)
		}
	}
	if successes != 1 || limits != 1 {
		t.Fatalf("successes/limits = %d/%d", successes, limits)
	}
}
