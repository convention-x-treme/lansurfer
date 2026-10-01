package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
)

// PostMessage runs under the request row lock like Leave and the lifecycle
// changes, so a message can never land after the poster left or the request
// closed.
func (s *Store) PostMessage(ctx context.Context, requestID, userID, body string) (gamerequest.Message, error) {
	databaseRequestID, databaseUserID, err := parseIDs(requestID, userID)
	if err != nil {
		return gamerequest.Message{}, gamerequest.ErrNotFound
	}
	var message gamerequest.Message
	err = s.withLockedRequest(ctx, databaseRequestID, func(queries *sqlcgen.Queries, locked sqlcgen.LockGameRequestRow) error {
		isMember, err := queries.GameRequestMemberExists(ctx, sqlcgen.GameRequestMemberExistsParams{
			GameRequestID: databaseRequestID, UserID: databaseUserID,
		})
		if err != nil {
			return fmt.Errorf("check game request membership: %w", err)
		}
		if err := chatAllowed(isMember, locked.Status); err != nil {
			return err
		}
		row, err := queries.CreateGameRequestMessage(ctx, sqlcgen.CreateGameRequestMessageParams{
			GameRequestID: databaseRequestID, AuthorID: databaseUserID, Body: body,
		})
		if err != nil {
			return fmt.Errorf("create game request message: %w", err)
		}
		message = gamerequest.Message{
			ID: row.ID, RequestID: row.GameRequestID, AuthorID: row.AuthorID,
			AuthorGameNick: row.AuthorGameNick, Body: row.Body, CreatedAt: row.CreatedAt.Time,
		}
		return nil
	})
	return message, err
}

func (s *Store) Messages(ctx context.Context, requestID, userID string) ([]gamerequest.Message, error) {
	if err := s.ChatAccess(ctx, requestID, userID); err != nil {
		return nil, err
	}
	databaseRequestID, _, err := parseIDs(requestID, userID)
	if err != nil {
		return nil, gamerequest.ErrNotFound
	}
	rows, err := s.queries.ListGameRequestMessages(ctx, databaseRequestID)
	if err != nil {
		return nil, fmt.Errorf("list game request messages: %w", err)
	}
	messages := make([]gamerequest.Message, len(rows))
	for i, row := range rows {
		messages[i] = gamerequest.Message{
			ID: row.ID, RequestID: row.GameRequestID, AuthorID: row.AuthorID,
			AuthorGameNick: row.AuthorGameNick, Body: row.Body, CreatedAt: row.CreatedAt.Time,
		}
	}
	return messages, nil
}

func (s *Store) ChatAccess(ctx context.Context, requestID, userID string) error {
	databaseRequestID, databaseUserID, err := parseIDs(requestID, userID)
	if err != nil {
		return gamerequest.ErrNotFound
	}
	access, err := s.queries.ChatAccess(ctx, sqlcgen.ChatAccessParams{
		GameRequestID: databaseRequestID, UserID: databaseUserID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return gamerequest.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("check game request chat access: %w", err)
	}
	return chatAllowed(access.IsMember, access.Status)
}

func chatAllowed(isMember bool, status string) error {
	if !isMember {
		return gamerequest.ErrNotMember
	}
	if status != string(gamerequest.StatusOpen) && status != string(gamerequest.StatusPlaying) {
		return gamerequest.ErrChatClosed
	}
	return nil
}
