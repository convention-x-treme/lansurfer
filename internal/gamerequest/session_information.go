package gamerequest

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"
)

const maxSessionInformationEntries = 12

func normalizeSessionInformationInput(input SessionInformationInput) (SessionInformationInput, error) {
	input.Key, input.Value = strings.TrimSpace(input.Key), strings.TrimSpace(input.Value)
	if input.Key == "" || utf8.RuneCountInString(input.Key) > 40 || strings.ContainsAny(input.Key, "\r\n") {
		return SessionInformationInput{}, ErrInvalidSessionInformationKey
	}
	if input.Value == "" || utf8.RuneCountInString(input.Value) > 300 || strings.ContainsAny(input.Value, "\r\n") {
		return SessionInformationInput{}, ErrInvalidSessionInformationValue
	}
	return input, nil
}

func (s *Service) SessionInformation(ctx context.Context, requestID, userID string) (SessionInformation, error) {
	return s.repository.SessionInformation(ctx, requestID, userID)
}

func (s *Service) CreateSessionInformationEntry(ctx context.Context, requestID, userID string, input SessionInformationInput) error {
	input, err := normalizeSessionInformationInput(input)
	if err != nil {
		return err
	}
	if err := s.repository.CreateSessionInformationEntry(ctx, requestID, userID, input); err != nil {
		return err
	}
	s.publish(Event{Type: EventSessionInformationChanged, RequestID: requestID})
	return nil
}

func (s *Service) UpdateSessionInformationEntry(ctx context.Context, requestID, entryID, userID string, input SessionInformationInput) error {
	input, err := normalizeSessionInformationInput(input)
	if err != nil {
		return err
	}
	changed, err := s.repository.UpdateSessionInformationEntry(ctx, requestID, entryID, userID, input)
	if err != nil {
		return err
	}
	if changed {
		s.publish(Event{Type: EventSessionInformationChanged, RequestID: requestID})
	}
	return nil
}

func (s *Service) DeleteSessionInformationEntry(ctx context.Context, requestID, entryID, userID string) error {
	if err := s.repository.DeleteSessionInformationEntry(ctx, requestID, entryID, userID); err != nil {
		return err
	}
	s.publish(Event{Type: EventSessionInformationChanged, RequestID: requestID})
	return nil
}

func (s *Service) CanSessionInformation(ctx context.Context, requestID, userID string) (bool, error) {
	err := s.repository.SessionInformationAccess(ctx, requestID, userID)
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotMember) || errors.Is(err, ErrNotActive) {
		return false, nil
	}
	return err == nil, err
}
