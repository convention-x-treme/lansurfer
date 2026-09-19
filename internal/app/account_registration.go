package app

import (
	"context"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type sessionCreator interface {
	NewSession(context.Context, string) (auth.BrowserSession, error)
}

// AccountRegistration coordinates the one transaction that necessarily spans
// the user- and auth-owned write paths for a brand-new local account.
type AccountRegistration struct {
	pool     *pgxpool.Pool
	sessions sessionCreator
}

func NewAccountRegistration(pool *pgxpool.Pool, sessions sessionCreator) *AccountRegistration {
	return &AccountRegistration{pool: pool, sessions: sessions}
}

func (r *AccountRegistration) Register(ctx context.Context, pendingID, browserBinding, gameNick string) (user.User, auth.BrowserSession, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return user.User{}, auth.BrowserSession{}, fmt.Errorf("begin account registration: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	flows := auth.NewFlowService(authpostgres.NewFlowStoreDB(tx))
	pending, err := flows.Pending(ctx, pendingID, browserBinding)
	if err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	if pending.Operation != auth.FlowLogin {
		return user.User{}, auth.BrowserSession{}, auth.ErrFlowOperation
	}
	account, err := user.NewService(userpostgres.NewStoreDB(tx)).Register(ctx, gameNick)
	if err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	_, err = authpostgres.NewIdentityStoreDB(tx).Create(ctx, auth.Identity{
		UserID: account.ID, ProviderKind: pending.ProviderKind, ProviderKey: pending.ProviderKey,
		Subject: pending.Subject, DisplayName: pending.DisplayName, Email: pending.Email,
	})
	if err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	if err := flows.ConsumePending(ctx, pendingID, browserBinding); err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return user.User{}, auth.BrowserSession{}, fmt.Errorf("commit account registration: %w", err)
	}
	session, err := r.sessions.NewSession(ctx, account.ID)
	if err != nil {
		return account, auth.BrowserSession{}, err
	}
	return account, session, nil
}
