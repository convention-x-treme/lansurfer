// Package config loads technical application configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strings"
)

const (
	defaultListenAddr = ":8080"
	defaultBaseURL    = "http://localhost:8080"
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

type IGDB struct {
	ClientID     string
	ClientSecret string
}

// Config contains the technical settings required by the application foundation.
type Config struct {
	DatabaseURL    string
	ListenAddr     string
	BaseURL        string
	OIDC           *OIDC
	Discord        *Discord
	IGDB           *IGDB
	TrustedProxies []netip.Prefix
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
	igdb, err := loadIGDB()
	if err != nil {
		return Config{}, err
	}

	trustedProxies, err := loadTrustedProxies()
	if err != nil {
		return Config{}, err
	}

	return Config{
		DatabaseURL:    databaseURL,
		ListenAddr:     listenAddr,
		BaseURL:        baseURL,
		OIDC:           oidc,
		Discord:        discord,
		IGDB:           igdb,
		TrustedProxies: trustedProxies,
	}, nil
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

func loadIGDB() (*IGDB, error) {
	clientID := strings.TrimSpace(os.Getenv("LANSURFER_IGDB_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("LANSURFER_IGDB_CLIENT_SECRET"))
	if clientID == "" && clientSecret == "" {
		return nil, nil
	}
	if clientID == "" || clientSecret == "" {
		return nil, errors.New("LANSURFER_IGDB_CLIENT_ID and LANSURFER_IGDB_CLIENT_SECRET must be configured together")
	}
	return &IGDB{ClientID: clientID, ClientSecret: clientSecret}, nil
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
