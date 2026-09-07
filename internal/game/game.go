// Package game owns LANsurfer's authoritative local game catalog.
package game

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxNameLength   = 120
	maxSlugLength   = 120
	maxAliasLength  = 120
	maxSearchLength = 120
)

var (
	ErrNotFound      = errors.New("game not found")
	ErrAliasNotFound = errors.New("game alias not found")
	ErrAlreadyExists = errors.New("game or alias already exists")
	ErrInvalidName   = errors.New("invalid game name")
	ErrInvalidSlug   = errors.New("invalid game slug")
	ErrInvalidAlias  = errors.New("invalid game alias")
	ErrInvalidSearch = errors.New("invalid game search")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Alias struct {
	ID    string
	Value string
}

type Game struct {
	ID        string
	Name      string
	Slug      string
	Active    bool
	Aliases   []Alias
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Repository interface {
	List(context.Context, string) ([]Game, error)
	Create(context.Context, string, string) (Game, error)
	Update(context.Context, string, string, string) (Game, error)
	SetActive(context.Context, string, bool) error
	AddAlias(context.Context, string, string) (Alias, error)
	RemoveAlias(context.Context, string, string) error
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) List(ctx context.Context, search string) ([]Game, error) {
	search = strings.TrimSpace(search)
	if utf8.RuneCountInString(search) > maxSearchLength {
		return nil, ErrInvalidSearch
	}
	return s.repository.List(ctx, search)
}

func (s *Service) Create(ctx context.Context, name, slug string) (Game, error) {
	name, slug, err := validateGame(name, slug)
	if err != nil {
		return Game{}, err
	}
	return s.repository.Create(ctx, name, slug)
}

func (s *Service) Update(ctx context.Context, id, name, slug string) (Game, error) {
	name, slug, err := validateGame(name, slug)
	if err != nil {
		return Game{}, err
	}
	return s.repository.Update(ctx, id, name, slug)
}

func (s *Service) SetActive(ctx context.Context, id string, active bool) error {
	return s.repository.SetActive(ctx, id, active)
}

func (s *Service) AddAlias(ctx context.Context, gameID, value string) (Alias, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > maxAliasLength {
		return Alias{}, ErrInvalidAlias
	}
	return s.repository.AddAlias(ctx, gameID, value)
}

func (s *Service) RemoveAlias(ctx context.Context, gameID, aliasID string) error {
	return s.repository.RemoveAlias(ctx, gameID, aliasID)
}

func validateGame(name, slug string) (string, string, error) {
	name = strings.TrimSpace(name)
	slug = strings.TrimSpace(slug)
	if name == "" || utf8.RuneCountInString(name) > maxNameLength {
		return "", "", ErrInvalidName
	}
	if utf8.RuneCountInString(slug) > maxSlugLength || !slugPattern.MatchString(slug) {
		return "", "", ErrInvalidSlug
	}
	return name, slug, nil
}
