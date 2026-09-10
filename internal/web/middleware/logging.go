package middleware

import (
	"context"
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
			requestLogger.Info("HTTP request completed", "status", wrapped.Status(), "bytes", wrapped.BytesWritten(), "duration", time.Since(started))
		})
	}
}

func LogError(r *http.Request, message string, err error) {
	logger, _ := r.Context().Value(requestLoggerKey{}).(*slog.Logger)
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error(message, "error", err)
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
