package gamerequest

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const maxMessageLength = 1000

var (
	ErrInvalidMessage = errors.New("invalid chat message")
	// ErrChatClosed reports a request that is no longer open or playing.
	ErrChatClosed = errors.New("game request chat is closed")
	// ErrChatRateLimited reports a poster who sent too many messages recently.
	ErrChatRateLimited = errors.New("game request chat rate limit exceeded")
)

// ChatRate allows a poster Limit messages per Window in one request. It is
// enforced under the request row lock, so concurrent posts cannot slip past it.
type ChatRate struct {
	Limit  int
	Window time.Duration
}

// DefaultChatRate applies unless the operator configures another.
var DefaultChatRate = ChatRate{Limit: 5, Window: 20 * time.Second}

// EventChatMessage signals that a chat message was posted. It carries no
// message content, and delivery must be limited to chat participants.
const EventChatMessage EventType = "GameRequestChatMessage"

// Message is one chat entry of a single Game Request.
type Message struct {
	ID             string
	RequestID      string
	AuthorID       string
	AuthorGameNick string
	Body           string
	CreatedAt      time.Time
}

// PostMessage appends a message for a current member (the Host is always one)
// of an open or playing request. Non-members get ErrNotMember.
func (s *Service) PostMessage(ctx context.Context, requestID, userID, body string) (Message, error) {
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > maxMessageLength {
		return Message{}, ErrInvalidMessage
	}
	message, err := s.repository.PostMessage(ctx, requestID, userID, body)
	if err == nil {
		s.publish(Event{Type: EventChatMessage, RequestID: requestID})
	}
	return message, err
}

// CanReadLobby reports whether userID is a current request member.
func (s *Service) CanReadLobby(ctx context.Context, requestID, userID string) (bool, error) {
	err := s.repository.LobbyAccess(ctx, requestID, userID)
	if errors.Is(err, ErrNotMember) {
		return false, nil
	}
	return err == nil, err
}
