package igdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
)

const (
	defaultTokenURL    = "https://id.twitch.tv/oauth2/token"
	defaultGamesURL    = "https://api.igdb.com/v4/games"
	defaultPageSize    = 500
	minRequestInterval = 250 * time.Millisecond
	maxResponseBytes   = 8 << 20

	requestTimeout    = 30 * time.Second
	tokenExpiryMargin = time.Minute
)

const gameFields = "id,name,slug,game_type.type,first_release_date,parent_game,version_parent,platforms.name,alternative_names.name,cover.image_id,updated_at,checksum"

type Option func(*Client)

type Client struct {
	clientID         string
	clientSecret     string
	tokenURL         string
	gamesURL         string
	pageSize         int
	requestInterval  time.Duration
	maxResponseBytes int
	httpClient       *http.Client
	now              func() time.Time
	sleep            func(context.Context, time.Duration) error

	tokenMu        sync.Mutex
	accessToken    string
	tokenExpiresAt time.Time

	requestMu       sync.Mutex
	lastRequestTime time.Time
}

var _ game.ProviderCatalog = (*Client)(nil)

func NewClient(clientID, clientSecret string, options ...Option) *Client {
	client := &Client{
		clientID:         clientID,
		clientSecret:     clientSecret,
		tokenURL:         defaultTokenURL,
		gamesURL:         defaultGamesURL,
		pageSize:         defaultPageSize,
		requestInterval:  minRequestInterval,
		maxResponseBytes: maxResponseBytes,
		httpClient:       &http.Client{Timeout: requestTimeout},
		now:              time.Now,
		sleep:            sleepContext,
	}
	for _, option := range options {
		option(client)
	}
	return client
}

func WithTokenURL(tokenURL string) Option {
	return func(client *Client) {
		client.tokenURL = tokenURL
	}
}

func WithGamesURL(gamesURL string) Option {
	return func(client *Client) {
		client.gamesURL = gamesURL
	}
}

func WithPageSize(pageSize int) Option {
	return func(client *Client) {
		if pageSize > 0 {
			client.pageSize = pageSize
		}
	}
}

// withRequestInterval overrides the pacing between IGDB request starts.
// Unexported: production code must always keep at least minRequestInterval
// between request starts, and an exported option would let a caller bypass
// that pacing constraint entirely (including down to 0). Only this
// package's own tests, which need a faster interval to stay fast, may use
// it.
func withRequestInterval(interval time.Duration) Option {
	return func(client *Client) {
		if interval >= 0 {
			client.requestInterval = interval
		}
	}
}

func WithMaxResponseBytes(limit int) Option {
	return func(client *Client) {
		if limit > 0 {
			client.maxResponseBytes = limit
		}
	}
}

func withNow(now func() time.Time) Option {
	return func(client *Client) {
		client.now = now
	}
}

func withSleep(sleep func(context.Context, time.Duration) error) Option {
	return func(client *Client) {
		client.sleep = sleep
	}
}

func (c *Client) Games(ctx context.Context, consume func([]game.ProviderGame) error) error {
	lastID := int64(0)
	for {
		page, err := c.gamesPage(ctx, lastID)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			return nil
		}

		games := make([]game.ProviderGame, len(page))
		for i, providerGame := range page {
			games[i] = mapProviderGame(providerGame)
		}
		if err := consume(games); err != nil {
			return fmt.Errorf("consume IGDB games: %w", err)
		}
		if len(page) < c.pageSize {
			return nil
		}
		lastID = page[len(page)-1].ID
	}
}

func (c *Client) gamesPage(ctx context.Context, lastID int64) ([]igdbGame, error) {
	token, err := c.token(ctx)
	if err != nil {
		return nil, err
	}

	page, status, err := c.requestGamesPage(ctx, token, lastID)
	if err != nil {
		return nil, err
	}
	if status != http.StatusUnauthorized {
		return page, statusError("IGDB games", status)
	}

	c.invalidateToken(token)
	token, err = c.token(ctx)
	if err != nil {
		return nil, err
	}
	page, status, err = c.requestGamesPage(ctx, token, lastID)
	if err != nil {
		return nil, err
	}
	return page, statusError("IGDB games", status)
}

func (c *Client) requestGamesPage(ctx context.Context, token string, lastID int64) ([]igdbGame, int, error) {
	body := fmt.Sprintf("fields %s;\nwhere id > %d;\nsort id asc;\nlimit %d;", gameFields, lastID, c.pageSize)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.gamesURL, strings.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("build IGDB games request: %w", err)
	}
	request.Header.Set("Client-ID", c.clientID)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "text/plain")

	c.requestMu.Lock()
	defer c.requestMu.Unlock()
	if !c.lastRequestTime.IsZero() {
		wait := c.requestInterval - c.now().Sub(c.lastRequestTime)
		if wait > 0 {
			if err := c.sleep(ctx, wait); err != nil {
				return nil, 0, fmt.Errorf("pace IGDB games request: %w", err)
			}
		}
	}
	c.lastRequestTime = c.now()

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("request IGDB games: %w", err)
	}
	responseBody, err := c.readResponseBody(response)
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("read IGDB games response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, response.StatusCode, nil
	}

	var page []igdbGame
	if err := json.Unmarshal(responseBody, &page); err != nil {
		return nil, response.StatusCode, fmt.Errorf("decode IGDB games response: %w", err)
	}
	if page == nil {
		return nil, response.StatusCode, errors.New("decode IGDB games response: expected JSON array")
	}
	return page, response.StatusCode, nil
}

func (c *Client) token(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	if c.accessToken != "" && c.now().Add(tokenExpiryMargin).Before(c.tokenExpiresAt) {
		return c.accessToken, nil
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("build app access token request: %w", err)
	}
	query := request.URL.Query()
	query.Set("client_id", c.clientID)
	query.Set("client_secret", c.clientSecret)
	query.Set("grant_type", "client_credentials")
	request.URL.RawQuery = query.Encode()

	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", errors.New("request app access token failed")
	}
	responseBody, err := c.readResponseBody(response)
	if err != nil {
		return "", fmt.Errorf("read app access token response: %w", err)
	}
	if err := statusError("app access token", response.StatusCode); err != nil {
		return "", err
	}

	var token tokenResponse
	if err := json.Unmarshal(responseBody, &token); err != nil {
		return "", fmt.Errorf("decode app access token response: %w", err)
	}
	if token.AccessToken == "" || token.ExpiresIn <= 0 {
		return "", errors.New("decode app access token response: missing token or expiry")
	}

	c.accessToken = token.AccessToken
	c.tokenExpiresAt = c.now().Add(time.Duration(token.ExpiresIn) * time.Second)
	return c.accessToken, nil
}

func (c *Client) invalidateToken(token string) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.accessToken == token {
		c.accessToken = ""
		c.tokenExpiresAt = time.Time{}
	}
}

func (c *Client) readResponseBody(response *http.Response) ([]byte, error) {
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, int64(c.maxResponseBytes)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > c.maxResponseBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", c.maxResponseBytes)
	}
	return body, nil
}

func statusError(operation string, status int) error {
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return nil
	}
	return fmt.Errorf("%s returned HTTP status %d", operation, status)
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func mapProviderGame(source igdbGame) game.ProviderGame {
	return game.ProviderGame{
		Reference:         game.ProviderReference{Provider: "igdb", ExternalID: strconv.FormatInt(source.ID, 10)},
		Name:              source.Name,
		Slug:              source.Slug,
		GameType:          source.GameType.Type,
		FirstReleaseDate:  unixTime(source.FirstReleaseDate),
		ParentExternalID:  optionalExternalID(source.ParentGame),
		VersionParentID:   optionalExternalID(source.VersionParent),
		PlatformNames:     names(source.Platforms),
		AlternativeNames:  names(source.AlternativeNames),
		CoverImageID:      source.Cover.ImageID,
		ProviderUpdatedAt: unixTime(source.UpdatedAt),
		Checksum:          source.Checksum,
	}
}

func unixTime(seconds *int64) *time.Time {
	if seconds == nil {
		return nil
	}
	value := time.Unix(*seconds, 0).UTC()
	return &value
}

func optionalExternalID(id *int64) string {
	if id == nil {
		return ""
	}
	return strconv.FormatInt(*id, 10)
}

func names(source []namedValue) []string {
	if source == nil {
		return nil
	}
	result := make([]string, len(source))
	for i, value := range source {
		result[i] = value.Name
	}
	return result
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

type igdbGame struct {
	ID               int64        `json:"id"`
	Name             string       `json:"name"`
	Slug             string       `json:"slug"`
	GameType         gameType     `json:"game_type"`
	FirstReleaseDate *int64       `json:"first_release_date"`
	ParentGame       *int64       `json:"parent_game"`
	VersionParent    *int64       `json:"version_parent"`
	Platforms        []namedValue `json:"platforms"`
	AlternativeNames []namedValue `json:"alternative_names"`
	Cover            cover        `json:"cover"`
	UpdatedAt        *int64       `json:"updated_at"`
	Checksum         string       `json:"checksum"`
}

type gameType struct {
	Type string `json:"type"`
}

type namedValue struct {
	Name string `json:"name"`
}

type cover struct {
	ImageID string `json:"image_id"`
}
