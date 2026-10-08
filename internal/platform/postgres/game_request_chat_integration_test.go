//go:build integration

package postgres

import (
	"errors"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	requestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
)

// Chat access follows current membership and the active lifecycle, and the
// authorization lives in the same locked transaction as the insert.
func TestGameRequestChatAccess(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.create(t, "Chat")
	other := fixture.create(t, "Other chat")
	ctx := t.Context()

	if _, err := fixture.requests.PostMessage(ctx, request.ID, fixture.outsider, "hi"); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("outsider post error = %v, want ErrNotMember", err)
	}
	if _, err := requestquery.NewStore(fixture.pool).Lobby(ctx, request.ID, fixture.outsider); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("outsider Lobby error = %v, want ErrNotMember", err)
	}
	if _, err := fixture.requests.PostMessage(ctx, request.ID, fixture.host, "  "); !errors.Is(err, gamerequest.ErrInvalidMessage) {
		t.Fatalf("blank post error = %v, want ErrInvalidMessage", err)
	}

	first, err := fixture.requests.PostMessage(ctx, request.ID, fixture.host, " meet at row 3 ")
	if err != nil {
		t.Fatalf("host post: %v", err)
	}
	if first.Body != "meet at row 3" || first.AuthorGameNick != "Playing Host" || first.CreatedAt.IsZero() {
		t.Fatalf("posted message = %+v", first)
	}
	if err := fixture.requests.Join(ctx, request.ID, fixture.guest); err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, err := fixture.requests.PostMessage(ctx, request.ID, fixture.guest, "on my way"); err != nil {
		t.Fatalf("guest post: %v", err)
	}
	if _, err := fixture.requests.PostMessage(ctx, other.ID, fixture.host, "elsewhere"); err != nil {
		t.Fatalf("other request post: %v", err)
	}

	lobby, err := requestquery.NewStore(fixture.pool).Lobby(ctx, request.ID, fixture.guest)
	if err != nil {
		t.Fatalf("guest read: %v", err)
	}
	var bodies []string
	for _, entry := range lobby.Entries {
		if entry.Source == requestquery.LobbySourceChat {
			bodies = append(bodies, entry.Message.Body)
		}
	}
	if len(bodies) != 2 || bodies[0] != "meet at row 3" || bodies[1] != "on my way" {
		t.Fatalf("chat bodies = %v, want the two messages of this request in order", bodies)
	}

	// Playing keeps chat usable.
	if err := fixture.requests.Start(ctx, request.ID, fixture.host); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := fixture.requests.PostMessage(ctx, request.ID, fixture.guest, "gl hf"); err != nil {
		t.Fatalf("post while playing: %v", err)
	}

	// Leaving cuts access immediately (only possible while open).
	if err := fixture.requests.Leave(ctx, other.ID, fixture.guest); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("leave non-joined: %v", err)
	}
	if err := fixture.requests.Join(ctx, other.ID, fixture.guest); err != nil {
		t.Fatalf("join other: %v", err)
	}
	if err := fixture.requests.Leave(ctx, other.ID, fixture.guest); err != nil {
		t.Fatalf("leave other: %v", err)
	}
	if _, err := fixture.requests.PostMessage(ctx, other.ID, fixture.guest, "late"); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("post after leave = %v, want ErrNotMember", err)
	}
	if allowed, err := fixture.requests.CanReadLobby(ctx, other.ID, fixture.guest); err != nil || allowed {
		t.Fatalf("CanReadLobby after leave = %v, %v", allowed, err)
	}

	// Closed requests stop active coordination.
	if err := fixture.requests.Close(ctx, request.ID, fixture.host); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := fixture.requests.PostMessage(ctx, request.ID, fixture.host, "after"); !errors.Is(err, gamerequest.ErrChatClosed) {
		t.Fatalf("post after close = %v, want ErrChatClosed", err)
	}
	if lobby, err := requestquery.NewStore(fixture.pool).Lobby(ctx, request.ID, fixture.host); err != nil || len(lobby.Entries) == 0 {
		t.Fatalf("host Lobby after close = %d entries, %v", len(lobby.Entries), err)
	}
}

func TestGameRequestLobbyAccessStaysRevokedAfterLeavingAndClosing(t *testing.T) {
	fixture := newPlayingFixture(t)
	ctx := t.Context()
	request := fixture.create(t, "Leave then close")
	if err := fixture.requests.Join(ctx, request.ID, fixture.guest); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := fixture.requests.Leave(ctx, request.ID, fixture.guest); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if allowed, err := fixture.requests.CanReadLobby(ctx, request.ID, fixture.guest); err != nil || allowed {
		t.Fatalf("access after leave = %v, %v; want denied", allowed, err)
	}
	if err := fixture.requests.Start(ctx, request.ID, fixture.host); err != nil {
		t.Fatalf("start after leave: %v", err)
	}
	if err := fixture.requests.Close(ctx, request.ID, fixture.host); err != nil {
		t.Fatalf("close after leave: %v", err)
	}
	if allowed, err := fixture.requests.CanReadLobby(ctx, request.ID, fixture.guest); err != nil || allowed {
		t.Fatalf("access after close = %v, %v; want denied", allowed, err)
	}
	if _, err := requestquery.NewStore(fixture.pool).Lobby(ctx, request.ID, fixture.guest); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("Lobby after close = %v; want ErrNotMember", err)
	}
}

func TestGameRequestLobbyAccessFollowsMembershipAcrossStatuses(t *testing.T) {
	fixture := newPlayingFixture(t)
	ctx := t.Context()
	for _, status := range []string{"open", "playing", "closed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			request := fixture.create(t, "Lobby "+status)
			if err := fixture.requests.Join(ctx, request.ID, fixture.guest); err != nil {
				t.Fatalf("join: %v", err)
			}
			if status == "playing" || status == "closed" {
				if err := fixture.requests.Start(ctx, request.ID, fixture.host); err != nil {
					t.Fatalf("start: %v", err)
				}
			}
			if status == "closed" {
				if err := fixture.requests.Close(ctx, request.ID, fixture.host); err != nil {
					t.Fatalf("close: %v", err)
				}
			}
			if status == "cancelled" {
				if err := fixture.requests.Cancel(ctx, request.ID, fixture.host); err != nil {
					t.Fatalf("cancel: %v", err)
				}
			}
			for _, userID := range []string{fixture.host, fixture.guest} {
				allowed, err := fixture.requests.CanReadLobby(ctx, request.ID, userID)
				if err != nil || !allowed {
					t.Fatalf("member %s access = %v, %v", userID, allowed, err)
				}
			}
			allowed, err := fixture.requests.CanReadLobby(ctx, request.ID, fixture.outsider)
			if err != nil || allowed {
				t.Fatalf("outsider access = %v, %v", allowed, err)
			}
		})
	}
}

// A poster is limited per request; others in the same request are unaffected.
func TestGameRequestChatRateLimit(t *testing.T) {
	fixture := newPlayingFixture(t)
	request := fixture.create(t, "Flood")
	ctx := t.Context()

	for i := 0; i < gamerequest.DefaultChatRate.Limit; i++ {
		if _, err := fixture.requests.PostMessage(ctx, request.ID, fixture.host, "spam"); err != nil {
			t.Fatalf("post %d: %v", i, err)
		}
	}
	if _, err := fixture.requests.PostMessage(ctx, request.ID, fixture.host, "spam"); !errors.Is(err, gamerequest.ErrChatRateLimited) {
		t.Fatalf("flood post error = %v, want ErrChatRateLimited", err)
	}
	if err := fixture.requests.Join(ctx, request.ID, fixture.guest); err != nil {
		t.Fatalf("join: %v", err)
	}
	if _, err := fixture.requests.PostMessage(ctx, request.ID, fixture.guest, "hello"); err != nil {
		t.Fatalf("other poster blocked: %v", err)
	}
}
