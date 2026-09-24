package igdb

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var jpegBytes = append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0}, 64)...)

func TestCoverFetcherRequestsFixedPathPerSize(t *testing.T) {
	for size, want := range map[string]string{
		"small": "/igdb/image/upload/t_cover_small/co1abc.jpg",
		"big":   "/igdb/image/upload/t_cover_big/co1abc.jpg",
	} {
		var got string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.URL.Path
			_, _ = w.Write(jpegBytes)
		}))
		image, err := newCoverFetcher(server.URL).Fetch(t.Context(), size, "co1abc")
		server.Close()
		if err != nil {
			t.Fatalf("Fetch(%s) error = %v", size, err)
		}
		if got != want || !bytes.Equal(image, jpegBytes) {
			t.Errorf("Fetch(%s) path = %q, want %q", size, got, want)
		}
	}
}

func TestCoverFetcherRejectsUnknownSize(t *testing.T) {
	if _, err := newCoverFetcher("http://127.0.0.1:1").Fetch(t.Context(), "medium", "co1abc"); err == nil {
		t.Fatal("Fetch(medium) error = nil")
	}
}

func TestCoverFetcherRejectsBadResponses(t *testing.T) {
	tests := map[string]http.HandlerFunc{
		"redirect": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://example.invalid/x.jpg", http.StatusFound)
		},
		"status": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write(jpegBytes)
		},
		"html": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("<!doctype html><html></html>"))
		},
		"oversize": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(append(jpegBytes, make([]byte, maxCoverBytes)...))
		},
	}
	for name, handler := range tests {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			if _, err := newCoverFetcher(server.URL).Fetch(t.Context(), "big", "co1abc"); err == nil {
				t.Fatal("Fetch() error = nil")
			}
		})
	}
}

func TestCoverFetcherTimesOut(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	fetcher := newCoverFetcher(server.URL)
	fetcher.client.Timeout = 50 * time.Millisecond
	if _, err := fetcher.Fetch(t.Context(), "big", "co1abc"); err == nil {
		t.Fatal("Fetch() error = nil, want timeout")
	}
}
