// Package config loads technical application configuration from the environment.
package config

import (
	"errors"
	"os"
	"strings"
)

const defaultListenAddr = ":8080"

// Config contains the technical settings required by the application foundation.
type Config struct {
	DatabaseURL string
	ListenAddr  string
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

	return Config{
		DatabaseURL: databaseURL,
		ListenAddr:  listenAddr,
	}, nil
}
