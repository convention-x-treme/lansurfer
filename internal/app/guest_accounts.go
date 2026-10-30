package app

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	authpostgres "github.com/convention-x-treme/lansurfer/internal/auth/postgres"
	queryuser "github.com/convention-x-treme/lansurfer/internal/query/user"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// GuestAccounts coordinates the transactions that span the user- and
// auth-owned write paths of shared-invite guests.
type GuestAccounts struct {
	pool          *pgxpool.Pool
	encryptionKey []byte
}

func NewGuestAccounts(pool *pgxpool.Pool, encryptionKey []byte) *GuestAccounts {
	return &GuestAccounts{pool: pool, encryptionKey: encryptionKey}
}

// Register creates a guest account, its guest identity, and a normal session
// in one transaction. The invite row stays locked until commit, so a signup
// that proceeds after rotation or disabling sees the new state.
func (g *GuestAccounts) Register(ctx context.Context, invite, gameNick string) (user.User, auth.BrowserSession, error) {
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return user.User{}, auth.BrowserSession{}, fmt.Errorf("begin guest registration: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	accessStore := authpostgres.NewGuestAccessStoreDB(tx)
	if err := accessStore.Lock(ctx); err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	valid, err := auth.NewGuestAccessService(accessStore, g.encryptionKey).Valid(ctx, invite)
	if err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	if !valid {
		return user.User{}, auth.BrowserSession{}, auth.ErrGuestInviteInvalid
	}
	users := user.NewService(userpostgres.NewStoreDB(tx))
	account, err := users.Register(ctx, gameNick)
	if err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	identity, err := auth.NewGuestIdentity(account.ID)
	if err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	if _, err := authpostgres.NewIdentityStoreDB(tx).Create(ctx, identity); err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	session, err := auth.NewSessionService(authpostgres.NewSessionStoreDB(tx), users).NewSession(ctx, account.ID)
	if err != nil {
		return user.User{}, auth.BrowserSession{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return user.User{}, auth.BrowserSession{}, fmt.Errorf("commit guest registration: %w", err)
	}
	return account, session, nil
}

// DeleteGuests completely deletes the selected accounts that still have a
// guest identity and returns their IDs. Permanent, converted, and missing
// accounts are skipped. Any failure rolls back the whole batch.
func (g *GuestAccounts) DeleteGuests(ctx context.Context, ids []string) ([]string, error) {
	selected, err := canonicalIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, user.ErrNotFound
	}
	return g.deleteGuests(ctx, func(pgx.Tx) ([]string, error) { return selected, nil })
}

// DeleteAllGuests deletes every account that is a guest when the operation
// reads the list.
func (g *GuestAccounts) DeleteAllGuests(ctx context.Context) ([]string, error) {
	return g.deleteGuests(ctx, func(tx pgx.Tx) ([]string, error) {
		guests, err := queryuser.NewStoreDB(tx).ListGuests(ctx)
		if err != nil {
			return nil, err
		}
		ids := make([]string, len(guests))
		for i, guest := range guests {
			ids[i] = guest.ID
		}
		return canonicalIDs(ids)
	})
}

func (g *GuestAccounts) deleteGuests(ctx context.Context, selection func(pgx.Tx) ([]string, error)) ([]string, error) {
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin guest deletion: %w", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	ids, err := selection(tx)
	if err != nil {
		return nil, err
	}
	userStore := userpostgres.NewStoreDB(tx)
	identities := authpostgres.NewIdentityStoreDB(tx)
	users := user.NewService(userStore)
	if err := userStore.LockAdministrators(ctx); err != nil {
		return nil, err
	}
	deleted := make([]string, 0, len(ids))
	for _, id := range ids {
		account, err := userStore.LockByID(ctx, id)
		if errors.Is(err, user.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		// A separate statement after the lock sees any conversion that won it.
		guest, err := identities.IsGuest(ctx, id)
		if err != nil {
			return nil, err
		}
		if !guest {
			continue
		}
		if err := users.DeleteAccount(ctx, id, account.GameNick); err != nil {
			return nil, err
		}
		deleted = append(deleted, id)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit guest deletion: %w", err)
	}
	return deleted, nil
}

// canonicalIDs validates UUIDs, then deduplicates and sorts them so locks are
// always taken in one order.
func canonicalIDs(ids []string) ([]string, error) {
	seen := make(map[string]struct{}, len(ids))
	canonical := make([]string, 0, len(ids))
	for _, id := range ids {
		var parsed pgtype.UUID
		if err := parsed.Scan(id); err != nil {
			return nil, user.ErrNotFound
		}
		value := parsed.String()
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		canonical = append(canonical, value)
	}
	sort.Strings(canonical)
	return canonical, nil
}
