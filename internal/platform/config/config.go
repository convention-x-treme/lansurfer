// Package config loads technical application configuration from the environment.
package config

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/convention-x-treme/lansurfer/internal/platform/crypto"
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

type IGDB struct {
	ClientID     string
	ClientSecret string
}

type WebPush struct {
	PublicKey  string
	PrivateKey string
	Subject    string
}

// Config contains the technical settings required by the application foundation.
type Config struct {
	DatabaseURL string
	ListenAddr  string
	BaseURL     string
	OIDC        *OIDC
	Discord     *Discord
	IGDB        *IGDB
	WebPush     *WebPush
	// DataEncryptionKey is the operator-provided AES-256 key used to encrypt
	// externally sourced credentials at rest (Web Push subscription material,
	// later Discord webhook tokens). Required when WebPush is configured;
	// otherwise nil until some other module needs it.
	DataEncryptionKey []byte
	TrustedProxies    []netip.Prefix
	// InstanceName is the trimmed, optional minimum instance identity from
	// LANSURFER_INSTANCE_NAME. Empty means unset; the web layer resolves the
	// display fallback ("LANsurfer"), the same way it already owns
	// branding.Default() rather than config owning product identity.
	InstanceName string
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
	webPush, err := loadWebPush()
	if err != nil {
		return Config{}, err
	}
	dataEncryptionKey, err := loadDataEncryptionKey()
	if err != nil {
		return Config{}, err
	}
	if webPush != nil && dataEncryptionKey == nil {
		return Config{}, errors.New("LANSURFER_DATA_ENCRYPTION_KEY is required when Web Push is configured")
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
		DatabaseURL:       databaseURL,
		ListenAddr:        listenAddr,
		BaseURL:           baseURL,
		OIDC:              oidc,
		Discord:           discord,
		IGDB:              igdb,
		WebPush:           webPush,
		DataEncryptionKey: dataEncryptionKey,
		TrustedProxies:    trustedProxies,
		InstanceName:      instanceName,
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

func loadWebPush() (*WebPush, error) {
	publicKey := strings.TrimSpace(os.Getenv("LANSURFER_WEBPUSH_PUBLIC_KEY"))
	privateKey := strings.TrimSpace(os.Getenv("LANSURFER_WEBPUSH_PRIVATE_KEY"))
	if publicKey == "" && privateKey == "" {
		return nil, nil
	}
	if publicKey == "" || privateKey == "" {
		return nil, errors.New("LANSURFER_WEBPUSH_PUBLIC_KEY and LANSURFER_WEBPUSH_PRIVATE_KEY must be configured together")
	}
	if err := validateVAPIDKeyPair(publicKey, privateKey); err != nil {
		return nil, fmt.Errorf("invalid VAPID key configuration: %w", err)
	}
	subject := strings.TrimSpace(os.Getenv("LANSURFER_WEBPUSH_SUBJECT"))
	if subject == "" {
		return nil, errors.New("LANSURFER_WEBPUSH_SUBJECT is required when Web Push is configured")
	}
	if !strings.HasPrefix(subject, "mailto:") && !strings.HasPrefix(subject, "https://") {
		return nil, errors.New(`LANSURFER_WEBPUSH_SUBJECT must start with "mailto:" or "https://"`)
	}
	return &WebPush{PublicKey: publicKey, PrivateKey: privateKey, Subject: subject}, nil
}

func validateVAPIDKeyPair(publicKey, privateKey string) error {
	publicBytes, err := decodeVAPIDComponent(publicKey)
	if err != nil {
		return fmt.Errorf("public key: %w", err)
	}
	privateBytes, err := decodeVAPIDComponent(privateKey)
	if err != nil {
		return fmt.Errorf("private key: %w", err)
	}
	if _, err := ecdh.P256().NewPublicKey(publicBytes); err != nil {
		return fmt.Errorf("public key is not a valid VAPID (P-256) key: %w", err)
	}
	if _, err := ecdh.P256().NewPrivateKey(privateBytes); err != nil {
		return fmt.Errorf("private key is not a valid VAPID (P-256) key: %w", err)
	}
	return nil
}

func decodeVAPIDComponent(value string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(value, "="))
	if err != nil {
		return nil, fmt.Errorf("not valid base64url: %w", err)
	}
	return decoded, nil
}

func loadDataEncryptionKey() ([]byte, error) {
	value := strings.TrimSpace(os.Getenv("LANSURFER_DATA_ENCRYPTION_KEY"))
	if value == "" {
		return nil, nil
	}
	key, err := crypto.ParseKey(value)
	if err != nil {
		return nil, fmt.Errorf("LANSURFER_DATA_ENCRYPTION_KEY: %w", err)
	}
	return key, nil
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
