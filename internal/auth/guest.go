package auth

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/convention-x-treme/lansurfer/internal/platform/crypto"
)

var (
	ErrGuestInviteInvalid      = errors.New("guest invite is invalid or disabled")
	ErrGuestAccessUnavailable  = errors.New("guest access unavailable")
	guestInviteEncodedLength   = base64.RawURLEncoding.EncodedLen(randomTokenBytes)
	errGuestAccessKeyMalformed = errors.New("guest access encryption key must be 32 bytes")
)

const guestProvider = "guest"

// GuestAccessState describes Guest Access and its public visibility.
type GuestAccessState struct {
	Enabled     bool
	ShowOnLogin bool
	Invite      string // Administrator presentation or explicitly enabled public entry redirect only.
}

// GuestAccessRecord is the stored singleton; the invite is encrypted.
type GuestAccessRecord struct {
	Enabled          bool
	ShowOnLogin      bool
	InviteCiphertext []byte
}

type GuestAccessRepository interface {
	State(context.Context) (GuestAccessRecord, error)
	SetShowOnLogin(context.Context, bool) error
	// SetEnabled sets the flag and initializes the invite ciphertext only when
	// none is stored yet.
	SetEnabled(context.Context, bool, []byte) error
	// Rotate replaces the invite ciphertext and leaves the flag unchanged.
	Rotate(context.Context, []byte) error
}

// GuestAccessService owns the single shared guest invite.
type GuestAccessService struct {
	repository GuestAccessRepository
	key        []byte
}

func NewGuestAccessService(repository GuestAccessRepository, key []byte) *GuestAccessService {
	return &GuestAccessService{repository: repository, key: key}
}

func (s *GuestAccessService) State(ctx context.Context) (GuestAccessState, error) {
	record, err := s.repository.State(ctx)
	if err != nil {
		return GuestAccessState{}, err
	}
	invite, err := s.decrypt(record)
	if err != nil {
		return GuestAccessState{}, err
	}
	return GuestAccessState{Enabled: record.Enabled, ShowOnLogin: record.ShowOnLogin, Invite: invite}, nil
}

func (s *GuestAccessService) SetShowOnLogin(ctx context.Context, show bool) error {
	return s.repository.SetShowOnLogin(ctx, show)
}

func (s *GuestAccessService) SetEnabled(ctx context.Context, enabled bool) error {
	if !enabled {
		return s.repository.SetEnabled(ctx, false, nil)
	}
	if len(s.key) != 32 {
		return ErrGuestAccessUnavailable
	}
	record, err := s.repository.State(ctx)
	if err != nil {
		return err
	}
	var ciphertext []byte
	if len(record.InviteCiphertext) == 0 {
		if ciphertext, err = s.newInvite(); err != nil {
			return err
		}
	}
	return s.repository.SetEnabled(ctx, true, ciphertext)
}

func (s *GuestAccessService) Rotate(ctx context.Context) error {
	if len(s.key) != 32 {
		return ErrGuestAccessUnavailable
	}
	// A wrong key or damaged invite must fail here, not be silently replaced.
	if _, err := s.State(ctx); err != nil {
		return err
	}
	ciphertext, err := s.newInvite()
	if err != nil {
		return err
	}
	return s.repository.Rotate(ctx, ciphertext)
}

// Valid reports whether invite is the current invite of enabled Guest Access.
// It reads the database on every call.
func (s *GuestAccessService) Valid(ctx context.Context, invite string) (bool, error) {
	presented, err := base64.RawURLEncoding.Strict().DecodeString(invite)
	if err != nil || len(invite) != guestInviteEncodedLength || len(presented) != randomTokenBytes {
		return false, nil
	}
	record, err := s.repository.State(ctx)
	if err != nil {
		return false, err
	}
	if !record.Enabled {
		return false, nil
	}
	current, err := s.decrypt(record)
	if err != nil {
		return false, err
	}
	currentBytes, err := base64.RawURLEncoding.Strict().DecodeString(current)
	if err != nil {
		return false, fmt.Errorf("decode stored guest invite: %w", err)
	}
	return subtle.ConstantTimeCompare(presented, currentBytes) == 1, nil
}

func (s *GuestAccessService) newInvite() ([]byte, error) {
	invite, err := randomToken()
	if err != nil {
		return nil, fmt.Errorf("generate guest invite: %w", err)
	}
	ciphertext, err := crypto.Encrypt(s.key, invite)
	if err != nil {
		return nil, fmt.Errorf("encrypt guest invite: %w", err)
	}
	return ciphertext, nil
}

func (s *GuestAccessService) decrypt(record GuestAccessRecord) (string, error) {
	if len(record.InviteCiphertext) == 0 {
		return "", nil
	}
	if len(s.key) != 32 {
		return "", errGuestAccessKeyMalformed
	}
	invite, err := crypto.Decrypt(s.key, record.InviteCiphertext)
	if err != nil {
		return "", fmt.Errorf("decrypt guest invite: %w", err)
	}
	return invite, nil
}

// NewGuestIdentity returns the marker identity of a shared-invite guest. Its
// subject is independent of the invite and unique per guest.
func NewGuestIdentity(userID string) (Identity, error) {
	subject, err := randomToken()
	if err != nil {
		return Identity{}, fmt.Errorf("generate guest identity: %w", err)
	}
	return Identity{UserID: userID, ProviderKind: guestProvider, ProviderKey: guestProvider, Subject: subject}, nil
}
