package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/convention-x-treme/lansurfer/internal/user"
	"golang.org/x/oauth2"
)

const (
	discordAuthorizationURL = "https://discord.com/oauth2/authorize"
	discordTokenURL         = "https://discord.com/api/oauth2/token"
	discordCurrentUserURL   = "https://discord.com/api/v10/users/@me"
	discordProfileLimit     = 1 << 20
)

type DiscordConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type DiscordProvider struct {
	oauth   oauth2.Config
	userURL string
}

func NewDiscordProvider(cfg DiscordConfig) *DiscordProvider {
	return &DiscordProvider{
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL,
			Endpoint: oauth2.Endpoint{AuthURL: discordAuthorizationURL, TokenURL: discordTokenURL},
			Scopes:   []string{"identify"},
		},
		userURL: discordCurrentUserURL,
	}
}

func (p *DiscordProvider) AuthorizationURL(state, _, _ string) string {
	return p.oauth.AuthCodeURL(state)
}

func (p *DiscordProvider) Exchange(ctx context.Context, code, _, _ string) (Profile, error) {
	token, err := p.oauth.Exchange(ctx, code)
	if err != nil {
		return Profile{}, fmt.Errorf("exchange Discord authorization code: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.userURL, nil)
	if err != nil {
		return Profile{}, fmt.Errorf("create Discord current-user request: %w", err)
	}
	response, err := p.oauth.Client(ctx, token).Do(request)
	if err != nil {
		return Profile{}, fmt.Errorf("fetch Discord current user: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, discordProfileLimit))
		return Profile{}, fmt.Errorf("fetch Discord current user: status %d", response.StatusCode)
	}
	var currentUser struct {
		ID         string  `json:"id"`
		Username   string  `json:"username"`
		GlobalName *string `json:"global_name"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, discordProfileLimit))
	if err := decoder.Decode(&currentUser); err != nil {
		return Profile{}, fmt.Errorf("decode Discord current user: %w", err)
	}
	currentUser.ID = strings.TrimSpace(currentUser.ID)
	currentUser.Username = strings.TrimSpace(currentUser.Username)
	if currentUser.ID == "" {
		return Profile{}, errors.New("Discord current user has no ID")
	}
	globalName := ""
	if currentUser.GlobalName != nil {
		globalName = strings.TrimSpace(*currentUser.GlobalName)
	}
	displayName := globalName
	if displayName == "" {
		displayName = currentUser.Username
	}
	return Profile{
		ProviderKind: "discord", ProviderKey: "discord", Subject: currentUser.ID,
		Name: displayName, PreferredUsername: currentUser.Username,
		GameNickSuggestion: discordGameNickSuggestion(globalName, currentUser.Username),
	}, nil
}

func discordGameNickSuggestion(globalName, username string) string {
	for _, candidate := range []string{globalName, username} {
		validated, err := user.ValidateGameNick(candidate)
		if err == nil {
			return validated
		}
	}
	return ""
}
