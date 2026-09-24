package igdb

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	// coverOrigin is fixed: no request input ever selects the upstream host.
	coverOrigin   = "https://images.igdb.com"
	coverTimeout  = 5 * time.Second
	maxCoverBytes = 1 << 20
)

var coverVariants = map[string]string{"small": "t_cover_small", "big": "t_cover_big"}

// CoverFetcher downloads one IGDB cover image from the fixed images.igdb.com
// origin for the same-origin cover proxy.
type CoverFetcher struct {
	origin string
	client *http.Client
}

func NewCoverFetcher() *CoverFetcher { return newCoverFetcher(coverOrigin) }

func newCoverFetcher(origin string) *CoverFetcher {
	return &CoverFetcher{origin: origin, client: &http.Client{
		Timeout: coverTimeout,
		// A redirect could point anywhere; treat it as a failed fetch.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (f *CoverFetcher) Fetch(ctx context.Context, size, imageID string) ([]byte, error) {
	variant, ok := coverVariants[size]
	if !ok {
		return nil, fmt.Errorf("unknown IGDB cover size %q", size)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet,
		f.origin+"/igdb/image/upload/"+variant+"/"+url.PathEscape(imageID)+".jpg", nil)
	if err != nil {
		return nil, fmt.Errorf("build IGDB cover request: %w", err)
	}
	response, err := f.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch IGDB cover: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch IGDB cover: status %d", response.StatusCode)
	}
	image, err := io.ReadAll(io.LimitReader(response.Body, maxCoverBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read IGDB cover: %w", err)
	}
	if len(image) > maxCoverBytes {
		return nil, fmt.Errorf("IGDB cover exceeds %d bytes", maxCoverBytes)
	}
	switch contentType := http.DetectContentType(image); contentType {
	case "image/jpeg", "image/png", "image/webp":
		return image, nil
	default:
		return nil, fmt.Errorf("IGDB cover has content type %q", contentType)
	}
}
