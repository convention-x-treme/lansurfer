package app

import (
	"context"
	"errors"
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
		// A new account starts with the creating identity's picture, if any.
		AvatarRef: pending.AvatarRef, AvatarSelected: pending.AvatarRef != "",
	})
	if errors.Is(err, auth.ErrIdentityExists) {
		// A concurrent registration for the same pending identity (e.g. two
		// tabs submitting the same onboarding form) already won; this
		// transaction rolls back (its own new User is discarded), and this
		// request signs into the account the winner just created instead of
		// failing outright.
		return r.resolveConcurrentRegistration(ctx, pending)
	}
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

// resolveConcurrentRegistration looks up the account a concurrent, identical
// registration request already committed, past this request's own rolled-
// back transaction, and signs this request into it too.
func (r *AccountRegistration) resolveConcurrentRegistration(ctx context.Context, pending auth.PendingIdentity) (user.User, auth.BrowserSession, error) {
	identity, err := authpostgres.NewIdentityStore(r.pool).ByProviderSubject(ctx, pending.ProviderKind, pending.ProviderKey, pending.Subject)
	if err != nil {
		return user.User{}, auth.BrowserSession{}, fmt.Errorf("resolve concurrent registration: %w", err)
	}
	account, err := user.NewService(userpostgres.NewStore(r.pool)).ByID(ctx, identity.UserID)
	if err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	session, err := r.sessions.NewSession(ctx, account.ID)
	if err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	return account, session, nil
}
