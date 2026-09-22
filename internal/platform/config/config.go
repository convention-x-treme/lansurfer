// Package config loads technical application configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"net/mail"
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

	maxLegalEntityNameLength = 200
	maxLegalAddressLength    = 300
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

// Legal is the operator's legal identity shown on the Terms and Privacy
// pages. Each deployment is its own data controller, so this is operator
// configuration; it is deliberately not a branding surface.
type Legal struct {
	EntityName   string
	Address      string
	ContactEmail string
	ImprintURL   string
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
	// Legal is the trimmed, validated operator legal identity. Empty fields
	// mean unset; the pages render them as "not configured".
	Legal Legal
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

	legal, err := loadLegal()
	if err != nil {
		return Config{}, err
	}
	if discord != nil && (legal.EntityName == "" || legal.ContactEmail == "") {
		return Config{}, errors.New("LANSURFER_LEGAL_ENTITY_NAME and LANSURFER_LEGAL_CONTACT_EMAIL are required when Discord login is configured")
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
		Legal:          legal,
		CoverCacheDir:  strings.TrimSpace(os.Getenv("LANSURFER_COVER_CACHE_DIR")),
	}, nil
}

func loadInstanceName() (string, error) {
	name := strings.TrimSpace(os.Getenv("LANSURFER_INSTANCE_NAME"))
	return name, checkSingleLine("LANSURFER_INSTANCE_NAME", name, maxInstanceNameLength)
}

// checkSingleLine rejects control characters, Unicode line/paragraph
// separators, and values longer than max runes. Empty values pass.
func checkSingleLine(name, value string, max int) error {
	for _, r := range value {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return fmt.Errorf("%s must not contain control characters or line breaks", name)
		}
	}
	if length := utf8.RuneCountInString(value); length > max {
		return fmt.Errorf("%s must be %d Unicode characters or fewer, got %d", name, max, length)
	}
	return nil
}

func loadLegal() (Legal, error) {
	legal := Legal{
		EntityName:   strings.TrimSpace(os.Getenv("LANSURFER_LEGAL_ENTITY_NAME")),
		Address:      strings.TrimSpace(os.Getenv("LANSURFER_LEGAL_ADDRESS")),
		ContactEmail: strings.TrimSpace(os.Getenv("LANSURFER_LEGAL_CONTACT_EMAIL")),
		ImprintURL:   strings.TrimSpace(os.Getenv("LANSURFER_IMPRINT_URL")),
	}
	if err := checkSingleLine("LANSURFER_LEGAL_ENTITY_NAME", legal.EntityName, maxLegalEntityNameLength); err != nil {
		return Legal{}, err
	}
	if err := checkSingleLine("LANSURFER_LEGAL_ADDRESS", legal.Address, maxLegalAddressLength); err != nil {
		return Legal{}, err
	}
	if legal.ContactEmail != "" {
		address, err := mail.ParseAddress(legal.ContactEmail)
		if err != nil || address.Name != "" || address.Address != legal.ContactEmail {
			return Legal{}, errors.New("LANSURFER_LEGAL_CONTACT_EMAIL must be a bare email address")
		}
	}
	if legal.ImprintURL != "" {
		parsed, err := url.Parse(legal.ImprintURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			return Legal{}, errors.New("LANSURFER_IMPRINT_URL must be an absolute HTTP(S) URL")
		}
	}
	return legal, nil
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
