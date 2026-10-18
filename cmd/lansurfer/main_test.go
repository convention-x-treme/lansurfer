package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/convention-x-treme/lansurfer/internal/platform/config"
)

func TestNotificationProblemGameRequestID(t *testing.T) {
	for _, tc := range []struct {
		name    string
		problem notification.ProblemDelivery
		want    string
	}{
		{
			name:    "activity",
			problem: notification.ProblemDelivery{Activity: notification.Activity{GameRequestID: "activity-id"}},
			want:    "activity-id",
		},
		{
			name: "reminder",
			problem: notification.ProblemDelivery{
				Activity:      notification.Activity{GameRequestID: "activity-id"},
				StartReminder: &notification.StartReminder{GameRequestID: "reminder-id"},
			},
			want: "reminder-id",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := notificationProblemGameRequestID(tc.problem); got != tc.want {
				t.Fatalf("notificationProblemGameRequestID() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestPromoteUserID(t *testing.T) {
	userID, err := promoteUserID([]string{"--user-id", "2c359aee-7690-4975-a2f8-1730cf37f42b"})
	if err != nil || userID != "2c359aee-7690-4975-a2f8-1730cf37f42b" {
		t.Fatalf("promoteUserID() = %q, %v", userID, err)
	}

	for _, arguments := range [][]string{nil, {"--user-id", ""}, {"unexpected"}} {
		if _, err := promoteUserID(arguments); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("promoteUserID(%q) error = %v, want usage error", arguments, err)
		}
	}
}

func TestNotificationDeliveryID(t *testing.T) {
	deliveryID, err := notificationDeliveryID([]string{"--delivery-id", "42"})
	if err != nil || deliveryID != "42" {
		t.Fatalf("notificationDeliveryID() = %q, %v", deliveryID, err)
	}

	for _, arguments := range [][]string{nil, {"--delivery-id", ""}, {"unexpected"}} {
		if _, err := notificationDeliveryID(arguments); err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("notificationDeliveryID(%q) error = %v, want usage error", arguments, err)
		}
	}
}

func TestGenerateSecretsAcceptedByConfig(t *testing.T) {
	var out strings.Builder
	if err := run(context.Background(), []string{"admin", "notifications", "generate-secrets"}, slog.New(slog.DiscardHandler), &out); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LANSURFER_DATABASE_URL", "postgres://example")
	t.Setenv("LANSURFER_WEBPUSH_SUBJECT", "mailto:ops@example.test")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	for _, line := range lines {
		key, value, _ := strings.Cut(line, "=")
		t.Setenv(key, value)
	}
	if _, err := config.Load(); err != nil {
		t.Fatalf("config.Load() rejected generated secrets: %v", err)
	}
}

func TestVersionCommand(t *testing.T) {
	oldVersion, oldCommit := version, commit
	version, commit = "v0.2.0", "0123456789abcdef0123456789abcdef01234567"
	t.Cleanup(func() { version, commit = oldVersion, oldCommit })

	var out strings.Builder
	if err := run(context.Background(), []string{"version"}, slog.New(slog.DiscardHandler), &out); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "LANsurfer v0.2.0 (0123456789abcdef0123456789abcdef01234567)\n"; got != want {
		t.Fatalf("version output = %q, want %q", got, want)
	}
}

func TestHelpCommand(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out strings.Builder
			if err := run(context.Background(), args, slog.New(slog.DiscardHandler), &out); err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{
				"version", "healthcheck", "admin users", "admin promote --user-id <UUID>",
				"admin notifications problems", "admin notifications generate-secrets",
				"admin notifications retry --delivery-id <ID>",
				"admin notifications retry-uncertain --delivery-id <ID>",
			} {
				if !strings.Contains(out.String(), command) {
					t.Errorf("help output missing %q", command)
				}
			}
		})
	}
}

func TestHealthcheckCommand(t *testing.T) {
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		location string
		wantErr  bool
	}{
		{name: "ready", status: 200, body: "ready\n"},
		{name: "unavailable", status: 503, body: "not ready\n", wantErr: true},
		{name: "wrong body", status: 200, body: "ready-ish\n", wantErr: true},
		{name: "redirect", status: 302, location: "/ready", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.location != "" {
					http.Redirect(w, r, tc.location, tc.status)
					return
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()

			var out strings.Builder
			err := run(context.Background(), []string{"healthcheck", "--url", server.URL}, slog.New(slog.DiscardHandler), &out)
			if (err != nil) != tc.wantErr {
				t.Fatalf("healthcheck error = %v, want error %t", err, tc.wantErr)
			}
			if tc.wantErr && out.Len() != 0 {
				t.Fatalf("healthcheck wrote response data on failure: %q", out.String())
			}
			if !tc.wantErr && out.String() != "LANsurfer is ready.\n" {
				t.Fatalf("healthcheck output = %q", out.String())
			}
		})
	}
}

func TestHealthcheckCommandHonorsContextTimeout(t *testing.T) {
	requestCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(requestCanceled)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := run(ctx, []string{"healthcheck", "--url", server.URL}, slog.New(slog.DiscardHandler), io.Discard); err == nil {
		t.Fatal("healthcheck succeeded after request context timed out")
	}
	select {
	case <-requestCanceled:
	case <-time.After(time.Second):
		t.Fatal("healthcheck did not cancel the timed-out HTTP request")
	}
}
