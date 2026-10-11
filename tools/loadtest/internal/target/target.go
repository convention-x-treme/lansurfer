package target

import (
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var errInvalidTarget = errors.New("load-test target is not allowed")

func DatabaseConfig() (*pgxpool.Config, error) {
	dsn := os.Getenv("LANSURFER_LOADTEST_DATABASE_URL")
	if dsn == "" {
		return nil, errInvalidTarget
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errInvalidTarget
	}
	port := cfg.ConnConfig.Port
	if port == 0 {
		port = 5432
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || len(cfg.ConnConfig.Fallbacks) != 0 || port != 55433 || cfg.ConnConfig.Database != "lansurfer_loadtest" || cfg.ConnConfig.User != "lansurfer_loadtest" {
		return nil, errInvalidTarget
	}
	return cfg, nil
}

func ValidateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || (u.Path != "" && u.Path != "/") || strings.Contains(raw, "?") || strings.Contains(raw, "#") {
		return errInvalidTarget
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return errInvalidTarget
	}
	if u.Hostname() == "127.0.0.1" && (port == 18080 || port == 18081) || u.Hostname() == "lansurfer" && port == 8080 || u.Hostname() == "proxy" && port == 8080 {
		return nil
	}
	return errInvalidTarget
}
