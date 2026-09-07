// Package postgres provides PostgreSQL persistence for the event module.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/event"
	"github.com/convention-x-treme/lansurfer/internal/event/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
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

func (s *Store) List(ctx context.Context) ([]event.Event, error) {
	rows, err := s.queries.ListEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	result := make([]event.Event, 0, len(rows))
	for _, row := range rows {
		result = append(result, fromEventRow(row.ID, row.Name, row.StartsAt, row.EndsAt, row.IsCurrent, row.CreatedAt, row.UpdatedAt))
	}
	return result, nil
}

func (s *Store) Create(ctx context.Context, name string, startsAt, endsAt time.Time) (event.Event, error) {
	row, err := s.queries.CreateEvent(ctx, sqlcgen.CreateEventParams{
		Name:     name,
		StartsAt: pgtype.Timestamptz{Time: startsAt, Valid: true},
		EndsAt:   pgtype.Timestamptz{Time: endsAt, Valid: true},
	})
	if err != nil {
		return event.Event{}, fmt.Errorf("create event: %w", err)
	}
	return fromEventRow(row.ID, row.Name, row.StartsAt, row.EndsAt, row.IsCurrent, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Store) Update(ctx context.Context, id, name string, startsAt, endsAt time.Time) (event.Event, error) {
	databaseID, err := parseUUID(id)
	if err != nil {
		return event.Event{}, event.ErrNotFound
	}
	row, err := s.queries.UpdateEvent(ctx, sqlcgen.UpdateEventParams{
		ID:       databaseID,
		Name:     name,
		StartsAt: pgtype.Timestamptz{Time: startsAt, Valid: true},
		EndsAt:   pgtype.Timestamptz{Time: endsAt, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return event.Event{}, event.ErrNotFound
	}
	if err != nil {
		return event.Event{}, fmt.Errorf("update event: %w", err)
	}
	return fromEventRow(row.ID, row.Name, row.StartsAt, row.EndsAt, row.IsCurrent, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Store) SetCurrent(ctx context.Context, id string) error {
	databaseID, err := parseUUID(id)
	if err != nil {
		return event.ErrNotFound
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin current event transaction: %w", err)
	}
	defer tx.Rollback(ctx)
	queries := s.queries.WithTx(tx)
	if err := queries.ClearCurrentEvent(ctx); err != nil {
		return fmt.Errorf("clear current event: %w", err)
	}
	updated, err := queries.MarkCurrentEvent(ctx, databaseID)
	if err != nil {
		return fmt.Errorf("set current event: %w", err)
	}
	if updated == 0 {
		return event.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit current event: %w", err)
	}
	return nil
}

func fromEventRow(id, name string, startsAt, endsAt pgtype.Timestamptz, current bool, createdAt, updatedAt pgtype.Timestamptz) event.Event {
	return event.Event{ID: id, Name: name, StartsAt: startsAt.Time, EndsAt: endsAt.Time, IsCurrent: current, CreatedAt: createdAt.Time, UpdatedAt: updatedAt.Time}
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}
