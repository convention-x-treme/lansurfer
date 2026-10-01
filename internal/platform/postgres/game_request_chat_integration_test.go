//go:build integration

package postgres

import (
	"errors"
	"testing"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
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
	if _, err := fixture.requests.Messages(ctx, request.ID, fixture.outsider); !errors.Is(err, gamerequest.ErrNotMember) {
		t.Fatalf("outsider read error = %v, want ErrNotMember", err)
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

	messages, err := fixture.requests.Messages(ctx, request.ID, fixture.guest)
	if err != nil {
		t.Fatalf("guest read: %v", err)
	}
	if len(messages) != 2 || messages[0].Body != "meet at row 3" || messages[1].Body != "on my way" {
		t.Fatalf("messages = %+v, want the two messages of this request in order", messages)
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
	if allowed, err := fixture.requests.CanChat(ctx, other.ID, fixture.guest); err != nil || allowed {
		t.Fatalf("CanChat after leave = %v, %v", allowed, err)
	}

	// Closed requests stop active coordination.
	if err := fixture.requests.Close(ctx, request.ID, fixture.host); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := fixture.requests.PostMessage(ctx, request.ID, fixture.host, "after"); !errors.Is(err, gamerequest.ErrChatClosed) {
		t.Fatalf("post after close = %v, want ErrChatClosed", err)
	}
	if _, err := fixture.requests.Messages(ctx, request.ID, fixture.host); !errors.Is(err, gamerequest.ErrChatClosed) {
		t.Fatalf("read after close = %v, want ErrChatClosed", err)
	}
}
