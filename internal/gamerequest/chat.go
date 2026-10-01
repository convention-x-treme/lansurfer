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
)

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

// Messages returns the request's chat, oldest first, under the same access
// rules as PostMessage.
func (s *Service) Messages(ctx context.Context, requestID, userID string) ([]Message, error) {
	return s.repository.Messages(ctx, requestID, userID)
}

// CanChat reports whether userID may currently read the request's chat. Realtime
// delivery asks this per event so that leaving cuts access immediately.
func (s *Service) CanChat(ctx context.Context, requestID, userID string) (bool, error) {
	err := s.repository.ChatAccess(ctx, requestID, userID)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotMember) || errors.Is(err, ErrChatClosed) {
		return false, nil
	}
	return err == nil, err
}
