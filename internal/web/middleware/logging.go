package middleware

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

type requestLoggerKey struct{}

func RequestLogging(logger *slog.Logger, trustedProxies []netip.Prefix) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			requestLogger := logger.With(
				"request_id", chimiddleware.GetReqID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"client_ip", clientIP(r, trustedProxies),
			)
			wrapped := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(wrapped, r.WithContext(context.WithValue(r.Context(), requestLoggerKey{}, requestLogger)))
			message, outcome := requestOutcome(r.Context())
			requestLogger.Info(message, "outcome", outcome, "status", wrapped.Status(), "bytes", wrapped.BytesWritten(), "duration", time.Since(started))
		})
	}
}

// requestOutcome reads why a request ended from its context's cancellation
// cause rather than from the status code, which stays 200 for a response the
// client never received. It only works while RequestLogging runs inside
// RequestTimeout: the cause lives on the context that middleware derives.
func requestOutcome(ctx context.Context) (message string, outcome string) {
	switch cause := context.Cause(ctx); {
	case cause == nil:
		return "HTTP request completed", "completed"
	case errors.Is(cause, ErrRequestTimeout):
		return "HTTP request timed out", "timed-out"
	default:
		// net/http's disconnect cause is not a stable sentinel across
		// versions, so anything that is not our own timeout is the client
		// going away.
		return "HTTP request aborted", "client-aborted"
	}
}

// RequestLogger returns the per-request logger, already carrying the request
// ID, method, path and client IP. Handlers that log more than an error use it
// so their lines correlate with the request's own.
func RequestLogger(r *http.Request) *slog.Logger {
	logger, _ := r.Context().Value(requestLoggerKey{}).(*slog.Logger)
	if logger == nil {
		return slog.Default()
	}
	return logger
}

func LogError(r *http.Request, message string, err error) {
	RequestLogger(r).Error(message, "error", err)
}

func clientIP(r *http.Request, trustedProxies []netip.Prefix) string {
	peer := remoteIP(r.RemoteAddr)
	if !peer.IsValid() {
		return "unknown"
	}
	if !trustedIP(peer, trustedProxies) {
		return peer.String()
	}

	forwarded := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for index := len(forwarded) - 1; index >= 0; index-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(forwarded[index]))
		if err != nil {
			return peer.String()
		}
		candidate = candidate.Unmap()
		if !trustedIP(candidate, trustedProxies) {
			return candidate.String()
		}
		peer = candidate
	}
	return peer.String()
}

func remoteIP(address string) netip.Addr {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return ip.Unmap()
}

func trustedIP(ip netip.Addr, proxies []netip.Prefix) bool {
	for _, proxy := range proxies {
		if proxy.Contains(ip) {
			return true
		}
	}
	return false
}
