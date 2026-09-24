package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/convention-x-treme/lansurfer/internal/platform/mediaproxy"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/go-chi/chi/v5"
)

// MediaImages is one same-origin media proxy instance (IGDB covers or
// provider avatars).
type MediaImages interface {
	Get(ctx context.Context, bucket, imageID string) ([]byte, error)
}

type Media struct {
	covers  MediaImages
	avatars MediaImages
}

func NewMedia(covers, avatars MediaImages) *Media {
	return &Media{covers: covers, avatars: avatars}
}

// IGDBCover serves one same-origin IGDB cover. Every failure is a plain
// status the page's existing image fallback handles.
func (h *Media) IGDBCover(w http.ResponseWriter, r *http.Request) {
	serveMedia(w, r, h.covers, chi.URLParam(r, "size"), chi.URLParam(r, "imageID"))
}

// Avatar serves one selected provider avatar same-origin, so player browsers
// never contact Discord, Twitch, or OIDC image hosts.
func (h *Media) Avatar(w http.ResponseWriter, r *http.Request) {
	serveMedia(w, r, h.avatars, "avatar", chi.URLParam(r, "avatarKey"))
}

func serveMedia(w http.ResponseWriter, r *http.Request, images MediaImages, bucket, id string) {
	image, err := images.Get(r.Context(), bucket, id)
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		switch {
		case errors.Is(err, mediaproxy.ErrNotFound):
			w.WriteHeader(http.StatusNotFound)
		case errors.Is(err, mediaproxy.ErrBusy):
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
		case r.Context().Err() != nil:
			// The browser is gone; there is nobody to answer.
		default:
			webmiddleware.LogError(r, "serve "+bucket+" media failed", err)
			w.WriteHeader(http.StatusInternalServerError)
		}
		return
	}
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", http.DetectContentType(image))
	w.Header().Set("Content-Length", strconv.Itoa(len(image)))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(image)
	}
}
