package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (s *Store) SessionInformation(ctx context.Context, requestID, userID string) (gamerequest.SessionInformation, error) {
	rid, uid, err := parseIDs(requestID, userID)
	if err != nil {
		return gamerequest.SessionInformation{}, gamerequest.ErrNotFound
	}
	rows, err := s.queries.ReadGameRequestSessionInformation(ctx, sqlcgen.ReadGameRequestSessionInformationParams{GameRequestID: rid, ViewerID: uid})
	if err != nil {
		return gamerequest.SessionInformation{}, fmt.Errorf("read session information: %w", err)
	}
	if len(rows) == 0 {
		return gamerequest.SessionInformation{}, gamerequest.ErrNotFound
	}
	row := rows[0]
	if !isActive(row.Status) {
		return gamerequest.SessionInformation{}, gamerequest.ErrNotActive
	}
	if !row.IsMember {
		return gamerequest.SessionInformation{}, gamerequest.ErrNotMember
	}
	result := gamerequest.SessionInformation{CanEdit: row.CanEdit}
	for _, entry := range rows {
		if entry.EntryID == "" {
			continue
		}
		result.Entries = append(result.Entries, gamerequest.SessionInformationEntry{ID: entry.EntryID, RequestID: entry.RequestID, Key: entry.Key.String, Value: entry.Value.String, Sequence: entry.Sequence.Int64, CreatedAt: entry.CreatedAt.Time, UpdatedAt: entry.UpdatedAt.Time})
	}
	return result, nil
}

func (s *Store) SessionInformationAccess(ctx context.Context, requestID, userID string) error {
	rid, uid, err := parseIDs(requestID, userID)
	if err != nil {
		return gamerequest.ErrNotFound
	}
	row, err := s.queries.GameRequestSessionInformationAccess(ctx, sqlcgen.GameRequestSessionInformationAccessParams{GameRequestID: rid, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return gamerequest.ErrNotFound
	}
	if err != nil {
		return err
	}
	if !isActive(row.Status) {
		return gamerequest.ErrNotActive
	}
	if !row.IsMember {
		return gamerequest.ErrNotMember
	}
	return nil
}

func (s *Store) CreateSessionInformationEntry(ctx context.Context, requestID, userID string, input gamerequest.SessionInformationInput) error {
	rid, _, err := parseIDs(requestID, userID)
	if err != nil {
		return gamerequest.ErrNotFound
	}
	return s.withLockedRequest(ctx, rid, func(q *sqlcgen.Queries, locked sqlcgen.LockGameRequestRow) error {
		if locked.CreatorID != userID {
			return gamerequest.ErrNotCreator
		}
		if !isActive(locked.Status) {
			return gamerequest.ErrNotActive
		}
		count, err := q.CountGameRequestSessionEntries(ctx, rid)
		if err != nil {
			return err
		}
		if count >= 12 {
			return gamerequest.ErrSessionInformationLimit
		}
		_, err = q.CreateGameRequestSessionEntry(ctx, sqlcgen.CreateGameRequestSessionEntryParams{GameRequestID: rid, Key: input.Key, Value: input.Value})
		if isUniqueViolation(err) {
			return gamerequest.ErrSessionInformationKeyExists
		}
		if isForeignKeyViolation(err) {
			return gamerequest.ErrNotFound
		}
		return err
	})
}

func isUniqueViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}
func (s *Store) UpdateSessionInformationEntry(ctx context.Context, requestID, entryID, userID string, input gamerequest.SessionInformationInput) (bool, error) {
	rid, _, err := parseIDs(requestID, userID)
	if err != nil {
		return false, gamerequest.ErrNotFound
	}
	eid, err := parseUUID(entryID)
	if err != nil {
		return false, gamerequest.ErrSessionInformationEntryNotFound
	}
	changed := false
	err = s.withLockedRequest(ctx, rid, func(q *sqlcgen.Queries, locked sqlcgen.LockGameRequestRow) error {
		if locked.CreatorID != userID {
			return gamerequest.ErrNotCreator
		}
		if !isActive(locked.Status) {
			return gamerequest.ErrNotActive
		}
		current, err := q.GetGameRequestSessionEntry(ctx, sqlcgen.GetGameRequestSessionEntryParams{GameRequestID: rid, ID: eid})
		if errors.Is(err, pgx.ErrNoRows) {
			return gamerequest.ErrSessionInformationEntryNotFound
		}
		if err != nil {
			return err
		}
		if current.Key == input.Key && current.Value == input.Value {
			return nil
		}
		n, err := q.UpdateGameRequestSessionEntry(ctx, sqlcgen.UpdateGameRequestSessionEntryParams{GameRequestID: rid, ID: eid, Key: input.Key, Value: input.Value})
		if isUniqueViolation(err) {
			return gamerequest.ErrSessionInformationKeyExists
		}
		if err != nil {
			return err
		}
		if n != 1 {
			return gamerequest.ErrSessionInformationEntryNotFound
		}
		changed = true
		return nil
	})
	return changed && err == nil, err
}

func (s *Store) DeleteSessionInformationEntry(ctx context.Context, requestID, entryID, userID string) error {
	rid, _, err := parseIDs(requestID, userID)
	if err != nil {
		return gamerequest.ErrNotFound
	}
	eid, err := parseUUID(entryID)
	if err != nil {
		return gamerequest.ErrSessionInformationEntryNotFound
	}
	return s.withLockedRequest(ctx, rid, func(q *sqlcgen.Queries, locked sqlcgen.LockGameRequestRow) error {
		if locked.CreatorID != userID {
			return gamerequest.ErrNotCreator
		}
		if !isActive(locked.Status) {
			return gamerequest.ErrNotActive
		}
		n, err := q.DeleteGameRequestSessionEntry(ctx, sqlcgen.DeleteGameRequestSessionEntryParams{GameRequestID: rid, ID: eid})
		if err != nil {
			return err
		}
		if n != 1 {
			return gamerequest.ErrSessionInformationEntryNotFound
		}
		return nil
	})
}
