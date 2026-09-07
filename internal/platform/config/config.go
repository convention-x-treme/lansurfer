// Package config loads technical application configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

const (
	defaultListenAddr = ":8080"
	defaultBaseURL    = "http://localhost:8080"
)

type OIDC struct {
	Issuer       string
	ClientID     string
	ClientSecret string
}

// Config contains the technical settings required by the application foundation.
type Config struct {
	DatabaseURL string
	ListenAddr  string
	BaseURL     string
	OIDC        *OIDC
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

	return Config{
		DatabaseURL: databaseURL,
		ListenAddr:  listenAddr,
		BaseURL:     baseURL,
		OIDC:        oidc,
	}, nil
}

func loadOIDC() (*OIDC, error) {
	issuer := strings.TrimSpace(os.Getenv("LANSURFER_OIDC_ISSUER"))
	clientID := strings.TrimSpace(os.Getenv("LANSURFER_OIDC_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("LANSURFER_OIDC_CLIENT_SECRET"))
	if issuer == "" && clientID == "" && clientSecret == "" {
		return nil, nil
	}
	if issuer == "" || clientID == "" {
		return nil, errors.New("LANSURFER_OIDC_ISSUER and LANSURFER_OIDC_CLIENT_ID must be configured together")
	}
	return &OIDC{Issuer: issuer, ClientID: clientID, ClientSecret: clientSecret}, nil
}
