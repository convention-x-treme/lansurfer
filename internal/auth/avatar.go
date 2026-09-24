package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"syscall"
	"time"
)

const (
	maxAvatarRefLength = 2048
	avatarFetchTimeout = 5 * time.Second
	maxAvatarBytes     = 1 << 20
	discordAvatarBase  = "https://cdn.discordapp.com"
	twitchAvatarHost   = "static-cdn.jtvnw.net"
)

var discordAvatarHashPattern = regexp.MustCompile(`^(a_)?[0-9a-f]{32}$`)

// normalizeAvatarRef returns ref when it is usable for providerKind and ""
// otherwise. An unusable picture never fails authentication.
func normalizeAvatarRef(providerKind, ref string) string {
	if ref == "" || len(ref) > maxAvatarRefLength {
		return ""
	}
	switch providerKind {
	case "discord":
		if discordAvatarHashPattern.MatchString(ref) {
			return ref
		}
	case "twitch":
		if u, ok := avatarURL(ref); ok && u.Hostname() == twitchAvatarHost {
			return ref
		}
	case "oidc":
		if _, ok := avatarURL(ref); ok {
			return ref
		}
	}
	return ""
}

func avatarURL(ref string) (*url.URL, bool) {
	u, err := url.Parse(ref)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Port() != "" {
		return nil, false
	}
	return u, true
}

// AvatarKey derives the public cache key for one identity picture. A new
// provider picture yields a new key, so served bytes can be immutable.
func AvatarKey(providerKind, subject, avatarRef string) string {
	if avatarRef == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(providerKind + "|" + subject + "|" + avatarRef))
	return hex.EncodeToString(sum[:16])
}

// StoredAvatar is the selected identity metadata behind one avatar key.
type StoredAvatar struct {
	ProviderKind, Subject, AvatarRef string
}

type avatarCatalog interface {
	SelectedAvatarKnown(ctx context.Context, key string) (bool, error)
	SelectedAvatarByKey(ctx context.Context, key string) (StoredAvatar, error)
}

// AvatarSource is the avatar catalog and upstream fetcher for the shared
// media proxy. Upstream URLs come only from stored identity metadata.
type AvatarSource struct {
	catalog     avatarCatalog
	client      *http.Client
	discordBase string
}

func NewAvatarSource(catalog avatarCatalog) *AvatarSource {
	return newAvatarSource(catalog, discordAvatarBase, guardedDialer())
}

func newAvatarSource(catalog avatarCatalog, discordBase string, dialer *net.Dialer) *AvatarSource {
	return &AvatarSource{
		catalog:     catalog,
		discordBase: discordBase,
		client: &http.Client{
			Timeout: avatarFetchTimeout,
			// No environment proxy: the dial guard must see the real target.
			Transport: &http.Transport{Proxy: nil, DialContext: dialer.DialContext, ForceAttemptHTTP2: true},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// guardedDialer refuses non-public addresses at connect time, after DNS
// resolution, because OIDC picture hosts are operator-controlled.
func guardedDialer() *net.Dialer {
	return &net.Dialer{Timeout: avatarFetchTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
			return fmt.Errorf("avatar host address %s is not public", host)
		}
		return nil
	}}
}

// Known is the media proxy catalog check: only selected avatars resolve.
func (s *AvatarSource) Known(ctx context.Context, key string) (bool, error) {
	return s.catalog.SelectedAvatarKnown(ctx, key)
}

func (s *AvatarSource) Fetch(ctx context.Context, _, key string) ([]byte, error) {
	avatar, err := s.catalog.SelectedAvatarByKey(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("resolve avatar: %w", err)
	}
	target, err := s.upstreamURL(avatar)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("avatar upstream status %d", response.StatusCode)
	}
	image, err := io.ReadAll(io.LimitReader(response.Body, maxAvatarBytes+1))
	if err != nil {
		return nil, err
	}
	if len(image) > maxAvatarBytes {
		return nil, errors.New("avatar exceeds size limit")
	}
	switch http.DetectContentType(image) {
	case "image/jpeg", "image/png", "image/webp":
		return image, nil
	}
	return nil, errors.New("avatar is not a supported image")
}

func (s *AvatarSource) upstreamURL(avatar StoredAvatar) (string, error) {
	// Re-validate stored data so the upstream target never depends on
	// anything but a currently acceptable reference.
	if normalizeAvatarRef(avatar.ProviderKind, avatar.AvatarRef) == "" {
		return "", errors.New("stored avatar reference is not usable")
	}
	if avatar.ProviderKind == "discord" {
		return s.discordBase + "/avatars/" + url.PathEscape(avatar.Subject) + "/" + avatar.AvatarRef + ".png?size=128", nil
	}
	return avatar.AvatarRef, nil
}
