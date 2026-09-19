package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PendingIdentityAttacher coordinates auth-owned identity and flow writes in a
// single transaction after another provider has proven the local account.
type PendingIdentityAttacher struct {
	pool *pgxpool.Pool
}

func NewPendingIdentityAttacher(pool *pgxpool.Pool) *PendingIdentityAttacher {
	return &PendingIdentityAttacher{pool: pool}
}

func (a *PendingIdentityAttacher) AttachPending(ctx context.Context, pendingID, browserBinding, userID, proofKind, proofKey string) error {
	tx, err := a.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin pending identity attachment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	flows := auth.NewFlowService(NewFlowStoreDB(tx))
	pending, err := flows.Pending(ctx, pendingID, browserBinding)
	if err != nil {
		return err
	}
	if pending.ProviderKind == proofKind && pending.ProviderKey == proofKey {
		return auth.ErrProviderAlreadyLinked
	}

	identities := NewIdentityStoreDB(tx)
	linked, err := identities.ListByUser(ctx, userID)
	if err != nil {
		return err
	}
	for _, identity := range linked {
		if identity.ProviderKind == pending.ProviderKind && identity.ProviderKey == pending.ProviderKey {
			return auth.ErrProviderAlreadyLinked
		}
	}
	_, err = identities.Create(ctx, auth.Identity{
		UserID: userID, ProviderKind: pending.ProviderKind, ProviderKey: pending.ProviderKey,
		Subject: pending.Subject, DisplayName: pending.DisplayName, Email: pending.Email,
	})
	if errors.Is(err, auth.ErrIdentityExists) {
		return auth.ErrIdentityConflict
	}
	if err != nil {
		return err
	}
	if err := flows.ConsumePending(ctx, pendingID, browserBinding); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit pending identity attachment: %w", err)
	}
	return nil
}
