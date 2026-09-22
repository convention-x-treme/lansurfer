package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/convention-x-treme/lansurfer/internal/game/cover"
	webmiddleware "github.com/convention-x-treme/lansurfer/internal/web/middleware"
	"github.com/go-chi/chi/v5"
)

type CoverImages interface {
	Get(ctx context.Context, size, imageID string) ([]byte, error)
}

type Media struct {
	covers CoverImages
}

func NewMedia(covers CoverImages) *Media {
	return &Media{covers: covers}
}

// IGDBCover serves one same-origin IGDB cover. Every failure is a plain
// status the page's existing image fallback handles.
func (h *Media) IGDBCover(w http.ResponseWriter, r *http.Request) {
	image, err := h.covers.Get(r.Context(), chi.URLParam(r, "size"), chi.URLParam(r, "imageID"))
	if err != nil {
		w.Header().Set("Cache-Control", "no-store")
		switch {
		case errors.Is(err, cover.ErrNotFound):
			w.WriteHeader(http.StatusNotFound)
		case errors.Is(err, cover.ErrBusy):
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusServiceUnavailable)
		case r.Context().Err() != nil:
			// The browser is gone; there is nobody to answer.
		default:
			webmiddleware.LogError(r, "serve IGDB cover failed", err)
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
