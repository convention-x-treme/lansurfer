// Package config loads technical application configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	defaultListenAddr     = ":8080"
	defaultBaseURL        = "http://localhost:8080"
	maxInstanceNameLength = 64
)

type OIDC struct {
	Issuer        string
	ClientID      string
	ClientSecret  string
	DisplayName   string
	GameNickClaim string
}

type Discord struct {
	ClientID     string
	ClientSecret string
}

type Twitch struct {
	ClientID     string
	ClientSecret string
	AuthEnabled  bool
	IGDBEnabled  bool
}

// Config contains the technical settings required by the application foundation.
type Config struct {
	DatabaseURL    string
	ListenAddr     string
	BaseURL        string
	OIDC           *OIDC
	Discord        *Discord
	Twitch         *Twitch
	TrustedProxies []netip.Prefix
	// InstanceName is the trimmed, optional minimum instance identity from
	// LANSURFER_INSTANCE_NAME. Empty means unset; the web layer resolves the
	// display fallback ("LANsurfer"), the same way it already owns
	// branding.Default() rather than config owning product identity.
	InstanceName string
	// CoverCacheDir is the trimmed LANSURFER_COVER_CACHE_DIR. Empty means
	// IGDB covers are cached in a bounded in-process memory store instead.
	CoverCacheDir string
}

// Load reads configuration from environment variables.
func Load() (Config, error) {
	databaseURL := strings.TrimSpace(os.Getenv("LANSURFER_DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, errors.New("LANSURFER_DATABASE_URL is required")
	}

	listenAddr := strings.TrimSpace(os.Getenv("LANSURFER_LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = defaultListenAddr
	}
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("LANSURFER_BASE_URL")), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || (parsedBaseURL.Scheme != "http" && parsedBaseURL.Scheme != "https") ||
		parsedBaseURL.Host == "" || parsedBaseURL.User != nil || parsedBaseURL.RawQuery != "" ||
		parsedBaseURL.Fragment != "" || (parsedBaseURL.Path != "" && parsedBaseURL.Path != "/") {
		return Config{}, fmt.Errorf("LANSURFER_BASE_URL must be an absolute HTTP(S) origin")
	}

	oidc, err := loadOIDC()
	if err != nil {
		return Config{}, err
	}
	discord, err := loadDiscord()
	if err != nil {
		return Config{}, err
	}
	twitch, err := loadTwitch()
	if err != nil {
		return Config{}, err
	}

	trustedProxies, err := loadTrustedProxies()
	if err != nil {
		return Config{}, err
	}

	instanceName, err := loadInstanceName()
	if err != nil {
		return Config{}, err
	}

	return Config{
		DatabaseURL:    databaseURL,
		ListenAddr:     listenAddr,
		BaseURL:        baseURL,
		OIDC:           oidc,
		Discord:        discord,
		Twitch:         twitch,
		TrustedProxies: trustedProxies,
		InstanceName:   instanceName,
		CoverCacheDir:  strings.TrimSpace(os.Getenv("LANSURFER_COVER_CACHE_DIR")),
	}, nil
}

func loadInstanceName() (string, error) {
	name := strings.TrimSpace(os.Getenv("LANSURFER_INSTANCE_NAME"))
	if name == "" {
		return "", nil
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return "", fmt.Errorf("LANSURFER_INSTANCE_NAME must not contain control characters or line breaks")
		}
	}
	if length := utf8.RuneCountInString(name); length > maxInstanceNameLength {
		return "", fmt.Errorf("LANSURFER_INSTANCE_NAME must be %d Unicode characters or fewer, got %d", maxInstanceNameLength, length)
	}
	return name, nil
}

func loadTrustedProxies() ([]netip.Prefix, error) {
	value := strings.TrimSpace(os.Getenv("LANSURFER_TRUSTED_PROXIES"))
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	proxies := make([]netip.Prefix, 0, len(parts))
	for _, part := range parts {
		entry := strings.TrimSpace(part)
		prefix, err := netip.ParsePrefix(entry)
		if err != nil {
			ip, ipErr := netip.ParseAddr(entry)
			if ipErr != nil {
				return nil, fmt.Errorf("LANSURFER_TRUSTED_PROXIES entry %q must be an IP address or CIDR prefix", entry)
			}
			prefix = netip.PrefixFrom(ip, ip.BitLen())
		}
		proxies = append(proxies, prefix.Masked())
	}
	return proxies, nil
}

func loadTwitch() (*Twitch, error) {
	clientID := strings.TrimSpace(os.Getenv("LANSURFER_TWITCH_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("LANSURFER_TWITCH_CLIENT_SECRET"))
	authEnabled, err := optionalBool("LANSURFER_TWITCH_AUTH_ENABLED")
	if err != nil {
		return nil, err
	}
	igdbEnabled, err := optionalBool("LANSURFER_IGDB_ENABLED")
	if err != nil {
		return nil, err
	}
	if !authEnabled && !igdbEnabled && clientID == "" && clientSecret == "" {
		return nil, nil
	}
	if authEnabled || igdbEnabled {
		if clientID == "" || clientSecret == "" {
			return nil, errors.New("LANSURFER_TWITCH_CLIENT_ID and LANSURFER_TWITCH_CLIENT_SECRET are required when Twitch authentication or IGDB is enabled")
		}
	}
	return &Twitch{ClientID: clientID, ClientSecret: clientSecret, AuthEnabled: authEnabled, IGDBEnabled: igdbEnabled}, nil
}

func optionalBool(name string) (bool, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", name, err)
	}
	return parsed, nil
}

func loadOIDC() (*OIDC, error) {
	issuer := strings.TrimSpace(os.Getenv("LANSURFER_OIDC_ISSUER"))
	clientID := strings.TrimSpace(os.Getenv("LANSURFER_OIDC_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("LANSURFER_OIDC_CLIENT_SECRET"))
	displayName := strings.TrimSpace(os.Getenv("LANSURFER_OIDC_DISPLAY_NAME"))
	gameNickClaim := strings.TrimSpace(os.Getenv("LANSURFER_OIDC_GAME_NICK_CLAIM"))
	if issuer == "" && clientID == "" && clientSecret == "" {
		return nil, nil
	}
	if issuer == "" || clientID == "" {
		return nil, errors.New("LANSURFER_OIDC_ISSUER and LANSURFER_OIDC_CLIENT_ID must be configured together")
	}
	if displayName == "" {
		displayName = "OpenID Connect"
	}
	return &OIDC{
		Issuer: issuer, ClientID: clientID, ClientSecret: clientSecret,
		DisplayName: displayName, GameNickClaim: gameNickClaim,
	}, nil
}

func loadDiscord() (*Discord, error) {
	clientID := strings.TrimSpace(os.Getenv("LANSURFER_DISCORD_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("LANSURFER_DISCORD_CLIENT_SECRET"))
	if clientID == "" && clientSecret == "" {
		return nil, nil
	}
	if clientID == "" || clientSecret == "" {
		return nil, errors.New("LANSURFER_DISCORD_CLIENT_ID and LANSURFER_DISCORD_CLIENT_SECRET must be configured together")
	}
	return &Discord{ClientID: clientID, ClientSecret: clientSecret}, nil
}
