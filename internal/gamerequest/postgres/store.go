// Package postgres provides PostgreSQL persistence for the gamerequest module.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool    *pgxpool.Pool
	queries *sqlcgen.Queries
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, queries: sqlcgen.New(pool)}
}

func (s *Store) Create(ctx context.Context, request gamerequest.GameRequest) (gamerequest.GameRequest, error) {
	gameID, err := parseUUID(request.GameID)
	if err != nil {
		return gamerequest.GameRequest{}, gamerequest.ErrGameUnavailable
	}
	creatorID, err := parseUUID(request.CreatorID)
	if err != nil {
		return gamerequest.GameRequest{}, fmt.Errorf("parse creator ID: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return gamerequest.GameRequest{}, fmt.Errorf("begin create game request transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)
	row, err := queries.CreateGameRequest(ctx, sqlcgen.CreateGameRequestParams{
		GameID: gameID, CreatorID: creatorID,
		MaxPlayers:  nullableInt4(request.MaxPlayers),
		StartsAt:    pgtype.Timestamptz{Time: request.StartsAt, Valid: true},
		Description: request.Description,
	})
	if err != nil {
		return gamerequest.GameRequest{}, fmt.Errorf("create game request: %w", err)
	}
	requestID, err := parseUUID(row.ID)
	if err != nil {
		return gamerequest.GameRequest{}, fmt.Errorf("parse created game request ID: %w", err)
	}
	if err := queries.AddCreatorMembership(ctx, sqlcgen.AddCreatorMembershipParams{GameRequestID: requestID, UserID: creatorID}); err != nil {
		return gamerequest.GameRequest{}, fmt.Errorf("add creator membership: %w", err)
	}
	if err := recordActivity(ctx, queries, requestID, creatorID, gamerequest.ActivityCreated); err != nil {
		return gamerequest.GameRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return gamerequest.GameRequest{}, fmt.Errorf("commit create game request: %w", err)
	}
	return fromCreateRow(row), nil
}

func (s *Store) Join(ctx context.Context, requestID, userID string) error {
	databaseRequestID, databaseUserID, err := parseIDs(requestID, userID)
	if err != nil {
		return gamerequest.ErrNotFound
	}
	return s.withLockedRequest(ctx, databaseRequestID, func(queries *sqlcgen.Queries, locked sqlcgen.LockGameRequestRow) error {
		if locked.Status != string(gamerequest.StatusOpen) {
			return gamerequest.ErrNotOpen
		}
		exists, err := queries.GameRequestMemberExists(ctx, sqlcgen.GameRequestMemberExistsParams{
			GameRequestID: databaseRequestID, UserID: databaseUserID,
		})
		if err != nil {
			return fmt.Errorf("check game request membership: %w", err)
		}
		if exists {
			return gamerequest.ErrAlreadyMember
		}
		if locked.MaxPlayers.Valid {
			count, err := queries.CountGameRequestMembers(ctx, databaseRequestID)
			if err != nil {
				return fmt.Errorf("count game request members: %w", err)
			}
			if count >= int64(locked.MaxPlayers.Int32) {
				return gamerequest.ErrFull
			}
		}
		if err := queries.AddGameRequestMember(ctx, sqlcgen.AddGameRequestMemberParams{
			GameRequestID: databaseRequestID, UserID: databaseUserID,
		}); err != nil {
			if isForeignKeyViolation(err) {
				return gamerequest.ErrNotFound
			}
			return fmt.Errorf("add game request member: %w", err)
		}
		return recordActivity(ctx, queries, databaseRequestID, databaseUserID, gamerequest.ActivityJoined)
	})
}

func (s *Store) Leave(ctx context.Context, requestID, userID string) error {
	databaseRequestID, databaseUserID, err := parseIDs(requestID, userID)
	if err != nil {
		return gamerequest.ErrNotFound
	}
	return s.withLockedRequest(ctx, databaseRequestID, func(queries *sqlcgen.Queries, locked sqlcgen.LockGameRequestRow) error {
		if locked.Status != string(gamerequest.StatusOpen) {
			return gamerequest.ErrNotOpen
		}
		if locked.CreatorID == userID {
			return gamerequest.ErrCreatorCannotLeave
		}
		removed, err := queries.RemoveGameRequestMember(ctx, sqlcgen.RemoveGameRequestMemberParams{
			GameRequestID: databaseRequestID, UserID: databaseUserID,
		})
		if err != nil {
			return fmt.Errorf("remove game request member: %w", err)
		}
		if removed == 0 {
			return gamerequest.ErrNotMember
		}
		return recordActivity(ctx, queries, databaseRequestID, databaseUserID, gamerequest.ActivityLeft)
	})
}

// RecordView notes that a player opened a Game Request. A deleted request or
// an unparsable ID is not an error the viewer should ever see: the detail page
// they are reading rendered fine, and the navigation's "Recently viewed" list
// is not worth failing that page over.
func (s *Store) RecordView(ctx context.Context, requestID, userID string) error {
	databaseRequestID, databaseUserID, err := parseIDs(requestID, userID)
	if err != nil {
		return gamerequest.ErrNotFound
	}
	err = s.queries.RecordGameRequestView(ctx, sqlcgen.RecordGameRequestViewParams{
		UserID: databaseUserID, GameRequestID: databaseRequestID,
	})
	if isForeignKeyViolation(err) {
		return gamerequest.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("record game request view: %w", err)
	}
	return nil
}

func (s *Store) SetStatus(ctx context.Context, requestID, userID string, status gamerequest.Status) error {
	databaseRequestID, databaseUserID, err := parseIDs(requestID, userID)
	if err != nil {
		return gamerequest.ErrNotFound
	}
	return s.withLockedRequest(ctx, databaseRequestID, func(queries *sqlcgen.Queries, locked sqlcgen.LockGameRequestRow) error {
		if locked.CreatorID != userID {
			return gamerequest.ErrNotCreator
		}
		if locked.Status != string(gamerequest.StatusOpen) {
			return gamerequest.ErrNotOpen
		}
		updated, err := queries.UpdateGameRequestStatus(ctx, sqlcgen.UpdateGameRequestStatusParams{
			ID: databaseRequestID, CreatorID: databaseUserID, Status: string(status),
		})
		if err != nil {
			return fmt.Errorf("update game request status: %w", err)
		}
		if updated != 1 {
			return gamerequest.ErrNotOpen
		}
		return recordActivity(ctx, queries, databaseRequestID, databaseUserID, statusActivity(status))
	})
}

// recordActivity keeps the durable record inside the caller's transaction, so a
// change and the entry describing it either both commit or neither does.
func recordActivity(ctx context.Context, queries *sqlcgen.Queries, requestID, actorID pgtype.UUID, kind gamerequest.ActivityKind) error {
	if err := queries.RecordGameRequestActivity(ctx, sqlcgen.RecordGameRequestActivityParams{
		GameRequestID: requestID, ActorID: actorID, Kind: string(kind),
	}); err != nil {
		return fmt.Errorf("record game request activity: %w", err)
	}
	return nil
}

func statusActivity(status gamerequest.Status) gamerequest.ActivityKind {
	if status == gamerequest.StatusCancelled {
		return gamerequest.ActivityCancelled
	}
	return gamerequest.ActivityClosed
}

func (s *Store) withLockedRequest(ctx context.Context, requestID pgtype.UUID, change func(*sqlcgen.Queries, sqlcgen.LockGameRequestRow) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin game request transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)
	locked, err := queries.LockGameRequest(ctx, requestID)
	if errors.Is(err, pgx.ErrNoRows) {
		return gamerequest.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("lock game request: %w", err)
	}
	if err := change(queries, locked); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit game request transaction: %w", err)
	}
	return nil
}

func fromCreateRow(row sqlcgen.CreateGameRequestRow) gamerequest.GameRequest {
	return gamerequest.GameRequest{
		ID: row.ID, GameID: row.GameID, CreatorID: row.CreatorID,
		MaxPlayers: int4Pointer(row.MaxPlayers), StartsAt: row.StartsAt.Time,
		Description: row.Description, Status: gamerequest.Status(row.Status),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
	}
}

func nullableInt4(value *int32) pgtype.Int4 {
	if value == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: *value, Valid: true}
}

func int4Pointer(value pgtype.Int4) *int32 {
	if !value.Valid {
		return nil
	}
	result := value.Int32
	return &result
}

func parseIDs(requestID, userID string) (pgtype.UUID, pgtype.UUID, error) {
	parsedRequestID, err := parseUUID(requestID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	parsedUserID, err := parseUUID(userID)
	return parsedRequestID, parsedUserID, err
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

func isForeignKeyViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23503"
}
