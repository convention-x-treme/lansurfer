package postgres

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/auth"
	"github.com/convention-x-treme/lansurfer/internal/auth/postgres/sqlcgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FlowStore struct {
	queries *sqlcgen.Queries
}

func NewFlowStore(pool *pgxpool.Pool) *FlowStore {
	return &FlowStore{queries: sqlcgen.New(pool)}
}

func (s *FlowStore) CreateOAuth(ctx context.Context, flow auth.OAuthFlow) (auth.OAuthFlow, error) {
	if err := s.cleanup(ctx, time.Now()); err != nil {
		return auth.OAuthFlow{}, err
	}
	initiatingUserID, err := optionalUUID(flow.InitiatingUserID)
	if err != nil {
		return auth.OAuthFlow{}, fmt.Errorf("parse initiating user ID: %w", err)
	}
	parentFlowID, err := optionalUUID(flow.ParentFlowID)
	if err != nil {
		return auth.OAuthFlow{}, fmt.Errorf("parse parent flow ID: %w", err)
	}
	row, err := s.queries.CreateOAuthFlow(ctx, sqlcgen.CreateOAuthFlowParams{
		Operation: string(flow.Operation), ProviderKind: flow.ProviderKind, ProviderKey: flow.ProviderKey,
		InitiatingUserID: initiatingUserID, ParentFlowID: parentFlowID,
		StateHash: flow.StateHash[:], BrowserBindingHash: flow.BrowserBindingHash[:],
		Nonce: nullableText(flow.Nonce), PkceVerifier: nullableText(flow.PKCEVerifier),
		ExpiresAt: pgtype.Timestamptz{Time: flow.ExpiresAt, Valid: true},
	})
	if err != nil {
		return auth.OAuthFlow{}, fmt.Errorf("create OAuth flow: %w", err)
	}
	return oauthFromCreate(row), nil
}

func (s *FlowStore) ConsumeOAuth(ctx context.Context, callback auth.OAuthCallbackHashes) (auth.OAuthFlow, error) {
	initiatingUserID, err := optionalUUID(callback.InitiatingUserID)
	if err != nil {
		return auth.OAuthFlow{}, auth.ErrFlowInitiatingUser
	}
	row, err := s.queries.ConsumeOAuthFlow(ctx, sqlcgen.ConsumeOAuthFlowParams{
		Now: pgtype.Timestamptz{Time: callback.Now, Valid: true}, LookupStateHash: callback.StateHash[:],
		BrowserBindingHash: callback.BrowserBindingHash[:], ProviderKind: callback.ProviderKind,
		ProviderKey: callback.ProviderKey, Operation: string(callback.Operation), InitiatingUserID: initiatingUserID,
	})
	if err == nil {
		return oauthFromConsumed(row), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return auth.OAuthFlow{}, fmt.Errorf("consume OAuth flow: %w", err)
	}
	return auth.OAuthFlow{}, s.oauthFailure(ctx, callback)
}

func (s *FlowStore) oauthFailure(ctx context.Context, callback auth.OAuthCallbackHashes) error {
	row, err := s.queries.GetAuthFlowByState(ctx, callback.StateHash[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrFlowNotFound
	}
	if err != nil {
		return fmt.Errorf("inspect OAuth flow: %w", err)
	}
	if row.ConsumedAt.Valid {
		return auth.ErrFlowConsumed
	}
	if !row.ExpiresAt.Time.After(callback.Now) {
		return auth.ErrFlowExpired
	}
	if subtle.ConstantTimeCompare(row.BrowserBindingHash, callback.BrowserBindingHash[:]) != 1 {
		return auth.ErrFlowBrowser
	}
	if row.ProviderKind != callback.ProviderKind || row.ProviderKey != callback.ProviderKey {
		return auth.ErrFlowProvider
	}
	if row.Operation != string(callback.Operation) {
		return auth.ErrFlowOperation
	}
	if uuidString(row.InitiatingUserID) != callback.InitiatingUserID {
		return auth.ErrFlowInitiatingUser
	}
	return auth.ErrFlowNotFound
}

func (s *FlowStore) CreatePending(ctx context.Context, pending auth.PendingIdentity) (auth.PendingIdentity, error) {
	if err := s.cleanup(ctx, time.Now()); err != nil {
		return auth.PendingIdentity{}, err
	}
	initiatingUserID, err := optionalUUID(pending.InitiatingUserID)
	if err != nil {
		return auth.PendingIdentity{}, fmt.Errorf("parse initiating user ID: %w", err)
	}
	parentFlowID, err := optionalUUID(pending.ParentFlowID)
	if err != nil {
		return auth.PendingIdentity{}, fmt.Errorf("parse parent flow ID: %w", err)
	}
	row, err := s.queries.CreatePendingIdentity(ctx, sqlcgen.CreatePendingIdentityParams{
		Operation: string(pending.Operation), ProviderKind: pending.ProviderKind, ProviderKey: pending.ProviderKey,
		Subject: nullableText(pending.Subject), InitiatingUserID: initiatingUserID, ParentFlowID: parentFlowID,
		BrowserBindingHash: pending.BrowserBindingHash[:], DisplayName: nullableText(pending.DisplayName),
		Email: nullableText(pending.Email), GameNickSuggestion: nullableText(pending.GameNickSuggestion),
		ExpiresAt: pgtype.Timestamptz{Time: pending.ExpiresAt, Valid: true},
	})
	if err != nil {
		return auth.PendingIdentity{}, fmt.Errorf("create pending identity: %w", err)
	}
	return pendingFromCreate(row), nil
}

func (s *FlowStore) Pending(ctx context.Context, id string, browserHash [sha256.Size]byte, now time.Time) (auth.PendingIdentity, error) {
	databaseID, err := parseUUID(id)
	if err != nil {
		return auth.PendingIdentity{}, auth.ErrFlowNotFound
	}
	row, err := s.queries.GetPendingIdentity(ctx, databaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.PendingIdentity{}, auth.ErrFlowNotFound
	}
	if err != nil {
		return auth.PendingIdentity{}, fmt.Errorf("get pending identity: %w", err)
	}
	if row.ConsumedAt.Valid {
		return auth.PendingIdentity{}, auth.ErrFlowConsumed
	}
	if !row.ExpiresAt.Time.After(now) {
		return auth.PendingIdentity{}, auth.ErrFlowExpired
	}
	if subtle.ConstantTimeCompare(row.BrowserBindingHash, browserHash[:]) != 1 {
		return auth.PendingIdentity{}, auth.ErrFlowBrowser
	}
	return pendingFromGet(row), nil
}

func (s *FlowStore) ConsumePending(ctx context.Context, id string, browserHash [sha256.Size]byte, now time.Time) error {
	databaseID, err := parseUUID(id)
	if err != nil {
		return auth.ErrFlowNotFound
	}
	_, err = s.queries.ConsumePendingIdentity(ctx, sqlcgen.ConsumePendingIdentityParams{
		Now: pgtype.Timestamptz{Time: now, Valid: true}, ID: databaseID, BrowserBindingHash: browserHash[:],
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("consume pending identity: %w", err)
	}
	_, err = s.Pending(ctx, id, browserHash, now)
	return err
}

func (s *FlowStore) cleanup(ctx context.Context, now time.Time) error {
	if err := s.queries.DeleteExpiredAuthFlows(ctx, pgtype.Timestamptz{Time: now, Valid: true}); err != nil {
		return fmt.Errorf("clean expired auth flows: %w", err)
	}
	return nil
}

func optionalUUID(value string) (pgtype.UUID, error) {
	if value == "" {
		return pgtype.UUID{}, nil
	}
	return parseUUID(value)
}

func uuidString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return value.String()
}

func hashArray(value []byte) [sha256.Size]byte {
	var result [sha256.Size]byte
	copy(result[:], value)
	return result
}

func oauthFromCreate(row sqlcgen.CreateOAuthFlowRow) auth.OAuthFlow {
	return auth.OAuthFlow{ID: row.ID, Operation: auth.FlowOperation(row.Operation), ProviderKind: row.ProviderKind,
		ProviderKey: row.ProviderKey, InitiatingUserID: uuidString(row.InitiatingUserID), ParentFlowID: uuidString(row.ParentFlowID),
		StateHash: hashArray(row.StateHash), BrowserBindingHash: hashArray(row.BrowserBindingHash),
		Nonce: row.Nonce.String, PKCEVerifier: row.PkceVerifier.String, ExpiresAt: row.ExpiresAt.Time}
}

func oauthFromConsumed(row sqlcgen.ConsumeOAuthFlowRow) auth.OAuthFlow {
	return auth.OAuthFlow{ID: row.ID, Operation: auth.FlowOperation(row.Operation), ProviderKind: row.ProviderKind,
		ProviderKey: row.ProviderKey, InitiatingUserID: uuidString(row.InitiatingUserID), ParentFlowID: uuidString(row.ParentFlowID),
		StateHash: hashArray(row.StateHash), BrowserBindingHash: hashArray(row.BrowserBindingHash),
		Nonce: row.Nonce.String, PKCEVerifier: row.PkceVerifier.String, ExpiresAt: row.ExpiresAt.Time}
}

func pendingFromCreate(row sqlcgen.CreatePendingIdentityRow) auth.PendingIdentity {
	return auth.PendingIdentity{ID: row.ID, Operation: auth.FlowOperation(row.Operation), ProviderKind: row.ProviderKind,
		ProviderKey: row.ProviderKey, Subject: row.Subject.String, InitiatingUserID: uuidString(row.InitiatingUserID),
		ParentFlowID: uuidString(row.ParentFlowID), BrowserBindingHash: hashArray(row.BrowserBindingHash),
		DisplayName: row.DisplayName.String, Email: row.Email.String, GameNickSuggestion: row.GameNickSuggestion.String,
		ExpiresAt: row.ExpiresAt.Time}
}

func pendingFromGet(row sqlcgen.GetPendingIdentityRow) auth.PendingIdentity {
	return auth.PendingIdentity{ID: row.ID, Operation: auth.FlowOperation(row.Operation), ProviderKind: row.ProviderKind,
		ProviderKey: row.ProviderKey, Subject: row.Subject.String, InitiatingUserID: uuidString(row.InitiatingUserID),
		ParentFlowID: uuidString(row.ParentFlowID), BrowserBindingHash: hashArray(row.BrowserBindingHash),
		DisplayName: row.DisplayName.String, Email: row.Email.String, GameNickSuggestion: row.GameNickSuggestion.String,
		ExpiresAt: row.ExpiresAt.Time}
}
