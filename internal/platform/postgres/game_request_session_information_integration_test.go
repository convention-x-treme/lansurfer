//go:build integration

package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
)

func TestGameRequestSessionInformationReadAccess(t *testing.T) {
	f := newPlayingFixture(t); req := f.create(t, "session info")
	input := gamerequest.SessionInformationInput{Key: "Server", Value: "10.20.30.40:27015"}
	if err := f.requests.CreateSessionInformationEntry(t.Context(), req.ID, f.host, input); err != nil { t.Fatal(err) }
	if _, err := f.requests.SessionInformation(t.Context(), req.ID, f.outsider); !errors.Is(err, gamerequest.ErrNotMember) { t.Fatalf("outsider read = %v", err) }
	if err := f.requests.Join(t.Context(), req.ID, f.guest); err != nil { t.Fatal(err) }
	for _, user := range []string{f.host, f.guest} { got, err := f.requests.SessionInformation(t.Context(), req.ID, user); if err != nil || len(got.Entries) != 1 || got.Entries[0].Value != input.Value { t.Fatalf("read = %+v, %v", got, err) } }
	if err := f.requests.Leave(t.Context(), req.ID, f.guest); err != nil { t.Fatal(err) }
	if _, err := f.requests.SessionInformation(t.Context(), req.ID, f.guest); !errors.Is(err, gamerequest.ErrNotMember) { t.Fatalf("after leave = %v", err) }
	if err := f.requests.Join(t.Context(), req.ID, f.guest); err != nil { t.Fatal(err) }
	if err := f.requests.Start(t.Context(), req.ID, f.host); err != nil { t.Fatal(err) }
	if _, err := f.requests.SessionInformation(t.Context(), req.ID, f.guest); err != nil { t.Fatal(err) }
	if err := f.requests.Close(t.Context(), req.ID, f.host); err != nil { t.Fatal(err) }
	if _, err := f.requests.SessionInformation(t.Context(), req.ID, f.host); !errors.Is(err, gamerequest.ErrNotActive) { t.Fatalf("closed read = %v", err) }
}

func TestGameRequestSessionInformationCreateValidationAndDatabaseConstraints(t *testing.T) {
	f := newPlayingFixture(t); first := f.create(t, "first"); second := f.create(t, "second")
	if err := f.requests.CreateSessionInformationEntry(t.Context(), first.ID, f.host, gamerequest.SessionInformationInput{Key: " Server ", Value: "  host "}); err != nil { t.Fatal(err) }
	if err := f.requests.CreateSessionInformationEntry(t.Context(), first.ID, f.host, gamerequest.SessionInformationInput{Key: "server", Value: "duplicate"}); !errors.Is(err, gamerequest.ErrSessionInformationKeyExists) { t.Fatalf("duplicate = %v", err) }
	if err := f.requests.CreateSessionInformationEntry(t.Context(), second.ID, f.host, gamerequest.SessionInformationInput{Key: "server", Value: "another"}); err != nil { t.Fatal(err) }
	for _, pair := range [][2]string{{strings.Repeat("k",41),"v"},{"k",strings.Repeat("v",301)},{"k\nx","v"},{"k","v\r"},{"","v"},{"k",""}} {
		if _, err := f.pool.Exec(t.Context(), `INSERT INTO game_request_session_entries(game_request_id,key,value) VALUES ($1,$2,$3)`, first.ID,pair[0],pair[1]); err == nil { t.Errorf("database accepted key/value lengths %d/%d",len(pair[0]),len(pair[1])) }
	}
	var key, value string
	if err := f.pool.QueryRow(t.Context(), `SELECT key,value FROM game_request_session_entries WHERE game_request_id=$1`, first.ID).Scan(&key,&value); err != nil || key != "Server" || value != "host" { t.Fatalf("stored = %q/%q, %v",key,value,err) }
}
